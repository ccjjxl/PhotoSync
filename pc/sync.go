package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options controls a sync run.
type Options struct {
	OutDir        string
	Workers       int
	IncludeVideos bool
	Mode          SyncMode
	DryRun        bool
	Overwrite     bool
	Since         int64 // unix seconds; 0 = everything
	PageSize      int
	Quiet         bool
	Verbose       bool
	MaxRetries    int
	Flat          bool // drop folders, put everything in one directory
	// Label identifies the device in multi-device runs (log prefix).
	Label string
	// Folders restricts the run to these device folders (empty = whatever the
	// phone allows). Each folder is fetched with its own paginated query.
	Folders []string
}

// SyncMode selects full vs incremental behaviour.
type SyncMode int

const (
	// ModeIncremental transfers only new/changed files (default).
	ModeIncremental SyncMode = iota
	// ModeFull re-downloads everything, verifying existing files.
	ModeFull
	// ModeRepair re-downloads only files whose local copy is missing or corrupt.
	ModeRepair
)

func (m SyncMode) String() string {
	switch m {
	case ModeFull:
		return "全量"
	case ModeRepair:
		return "修复"
	default:
		return "增量"
	}
}

func loadInt64(p *int64) int64 { return atomic.LoadInt64(p) }

// Stats is the live progress state, safe for concurrent reads.
type Stats struct {
	TotalFiles   int64
	TotalBytes   int64
	DoneFiles    int64
	DoneBytes    int64
	LiveBytes    int64 // includes in-flight bytes, for smooth speed/ETA
	Skipped      int64
	Failed       int64
	Repaired     int64
	Started      time.Time
	CurrentFiles []string

	mu sync.Mutex
}

func (s *Stats) snapshotFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.CurrentFiles))
	copy(out, s.CurrentFiles)
	return out
}

func (s *Stats) addActive(name string) {
	s.mu.Lock()
	s.CurrentFiles = append(s.CurrentFiles, name)
	if len(s.CurrentFiles) > 8 {
		s.CurrentFiles = s.CurrentFiles[len(s.CurrentFiles)-8:]
	}
	s.mu.Unlock()
}

func (s *Stats) removeActive(name string) {
	s.mu.Lock()
	for i, f := range s.CurrentFiles {
		if f == name {
			s.CurrentFiles = append(s.CurrentFiles[:i], s.CurrentFiles[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
}

// Speed returns bytes/sec since start.
func (s *Stats) Speed() float64 {
	elapsed := time.Since(s.Started).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&s.LiveBytes)) / elapsed
}

// ETA estimates remaining time.
func (s *Stats) ETA() time.Duration {
	speed := s.Speed()
	if speed <= 0 {
		return 0
	}
	remaining := float64(atomic.LoadInt64(&s.TotalBytes) - atomic.LoadInt64(&s.LiveBytes))
	if remaining < 0 {
		remaining = 0
	}
	if remaining <= 0 {
		return 0
	}
	return time.Duration(remaining/speed) * time.Second
}

// Progress is a callback invoked on state changes; may be called concurrently.
type Progress func(s *Stats, event Event, item *MediaItem, err error, dst string)

// Event enumerates what happened to an item.
type Event int

const (
	EventDiscovered Event = iota
	EventStart
	EventDone
	EventFailed
	EventFinished
)

// Syncer performs the actual transfer.
type Syncer struct {
	client *Client
	opts   Options
	man    *Manifest
	stats  *Stats
	prog   Progress
}

// NewSyncer wires a client, manifest and progress reporter together.
func NewSyncer(client *Client, man *Manifest, opts Options, prog Progress) *Syncer {
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers()
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 300
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	return &Syncer{
		client: client,
		opts:   opts,
		man:    man,
		stats:  &Stats{Started: time.Now()},
		prog:   prog,
	}
}

func defaultWorkers() int {
	n := runtime.NumCPU()
	if n < 4 {
		n = 4
	}
	if n > 12 {
		n = 12
	}
	return n
}

// Stats exposes live counters.
func (s *Syncer) Stats() *Stats { return s.stats }

// Run executes the sync: discover -> filter -> parallel transfer.
func (s *Syncer) Run(ctx context.Context) error {
	// ---------------------------------------------------------- discovery phase
	var items []MediaItem
	since := s.opts.Since
	if s.opts.Mode == ModeFull {
		since = 0
	}

	// One paginated walk per requested folder (or a single walk over whatever
	// the phone allows when no folder was requested).
	folderList := s.opts.Folders
	if len(folderList) == 0 {
		folderList = []string{""}
	}

	for _, folderArg := range folderList {
		offset := 0
		for {
			page, err := s.client.List(ctx, since, offset, s.opts.PageSize, s.opts.IncludeVideos, folderArg)
			if err != nil {
				if errors.Is(err, ErrUnauthorized) {
					return err
				}
				// retry page a few times, transient Wi-Fi hiccups are normal
				var lastErr error = err
				ok := false
				for attempt := 1; attempt <= s.opts.MaxRetries; attempt++ {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
					}
					page, err = s.client.List(ctx, since, offset, s.opts.PageSize, s.opts.IncludeVideos, folderArg)
					if err == nil {
						ok = true
						break
					}
					lastErr = err
				}
				if !ok {
					return fmt.Errorf("获取列表失败: %w", lastErr)
				}
			}

			items = append(items, page.Items...)
			offset += len(page.Items)
			if len(page.Items) == 0 || !page.HasMore || offset >= page.Total {
				break
			}
			if len(page.Items) < s.opts.PageSize {
				break
			}
		}
	}

	// ---------------------------------------------------------- planning phase
	type job struct {
		item MediaItem
		dst  string
		size int64
	}
	var jobs []job
	var skipped int64

	for _, it := range items {
		if !s.opts.IncludeVideos && it.Video {
			skipped++
			continue
		}
		dst := s.localPath(it)

		existing, tracked := s.man.Get(it.Path)
		// Full mode re-downloads everything; repair ignores the manifest and
		// trusts only what is actually on disk.
		force := s.opts.Overwrite || s.opts.Mode == ModeFull
		trustManifest := s.opts.Mode != ModeRepair
		if !force {
			// Case A: file recorded in manifest and still on disk with matching size/mtime.
			if trustManifest && tracked && existing.Size == it.Size && existing.Mtime == it.Mtime {
				if info, err := os.Stat(filepath.Join(s.opts.OutDir, existing.Local)); err == nil &&
					info.Size() == it.Size {
					skipped++
					continue
				}
			}
			// Case B: file exists on disk (e.g. copied by another tool or manifest lost).
			if info, err := os.Stat(dst); err == nil && info.Size() == it.Size {
				rel, _ := filepath.Rel(s.opts.OutDir, dst)
				s.man.Put(it.Path, Entry{ID: it.ID, Size: it.Size, Mtime: it.Mtime, Local: rel})
				skipped++
				if s.opts.Verbose {
					fmt.Printf("= 跳过 %s（本地已存在）\n", it.Path)
				}
				continue
			}
		}

		jobs = append(jobs, job{item: it, dst: dst, size: it.Size})
	}

	atomic.StoreInt64(&s.stats.TotalFiles, int64(len(jobs)))
	var totalBytes int64
	for _, j := range jobs {
		totalBytes += j.size
	}
	atomic.StoreInt64(&s.stats.TotalBytes, totalBytes)
	atomic.StoreInt64(&s.stats.Skipped, skipped)

	if s.prog != nil {
		s.prog(s.stats, EventDiscovered, nil, nil, "")
	}

	if s.opts.DryRun || len(jobs) == 0 {
		if s.prog != nil {
			s.prog(s.stats, EventFinished, nil, nil, "")
		}
		return nil
	}

	// ---------------------------------------------------------- transfer phase
	jobCh := make(chan job, s.opts.Workers*2)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for i := 0; i < s.opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if ctx.Err() != nil {
					return
				}
				err := s.transfer(ctx, j.item, j.dst)
				if err != nil {
					atomic.AddInt64(&s.stats.Failed, 1)
					atomic.AddInt64(&s.stats.DoneFiles, 1)
					if s.prog != nil {
						s.prog(s.stats, EventFailed, &j.item, err, j.dst)
					}
					errMu.Lock()
					if firstErr == nil && ctx.Err() == nil {
						firstErr = err
					}
					errMu.Unlock()
					continue
				}
				atomic.AddInt64(&s.stats.DoneFiles, 1)
				atomic.AddInt64(&s.stats.DoneBytes, j.size)
				if s.prog != nil {
					s.prog(s.stats, EventDone, &j.item, nil, j.dst)
				}
			}
		}()
	}

feed:
	for _, j := range jobs {
		select {
		case <-ctx.Done():
			break feed
		case jobCh <- j:
		}
	}
	close(jobCh)
	wg.Wait()

	if err := s.man.SaveWithRetry(); err != nil && !s.opts.Quiet {
		fmt.Fprintf(os.Stderr, "警告: %v\n", err)
	}
	if s.prog != nil {
		s.prog(s.stats, EventFinished, nil, nil, "")
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// localPath builds the destination path, honouring folder structure and flat mode.
func (s *Syncer) localPath(it MediaItem) string {
	if s.opts.Flat {
		return filepath.Join(s.opts.OutDir, sanitizeName(it.Name))
	}
	rel := it.Path
	if rel == "" {
		rel = it.Name
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = sanitizePathComponent(p)
	}
	return filepath.Join(append([]string{s.opts.OutDir}, parts...)...)
}

// transfer downloads one item with resume + retries and writes it atomically.
func (s *Syncer) transfer(ctx context.Context, it MediaItem, dst string) error {
	s.stats.addActive(it.Name)
	defer s.stats.removeActive(it.Name)
	if s.prog != nil {
		s.prog(s.stats, EventStart, &it, nil, dst)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	partPath := dst + ".part"
	var lastErr error

	for attempt := 1; attempt <= s.opts.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		var offset int64
		if st, err := os.Stat(partPath); err == nil && st.Size() < it.Size {
			offset = st.Size()
		} else if err == nil && st.Size() >= it.Size {
			_ = os.Remove(partPath)
			offset = 0
		}

		err := s.downloadOnce(ctx, it, partPath, offset)
		if err == nil {
			// finalise
			if err := os.Rename(partPath, dst); err != nil {
				return fmt.Errorf("重命名失败: %w", err)
			}
			if it.Mtime > 0 {
				mt := time.Unix(it.Mtime, 0)
				_ = os.Chtimes(dst, mt, mt)
			}
			rel, _ := filepath.Rel(s.opts.OutDir, dst)
			s.man.Put(it.Path, Entry{ID: it.ID, Size: it.Size, Mtime: it.Mtime, Local: rel})
			return nil
		}
		lastErr = err

		if errors.Is(err, context.Canceled) {
			return err
		}
		if !isRetryable(err) {
			return err
		}
		// exponential backoff: 300ms, 900ms, 1.8s
		backoff := time.Duration(attempt) * 300 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	_ = os.Remove(partPath)
	return lastErr
}

type retryable struct{ err error }

func (r retryable) Error() string { return r.err.Error() }
func (r retryable) Unwrap() error { return r.err }

func isRetryable(err error) bool {
	var r retryable
	return errors.As(err, &r) || errors.Is(err, io.ErrUnexpectedEOF)
}

func retryErr(format string, args ...any) error {
	return retryable{fmt.Errorf(format, args...)}
}

// downloadOnce streams the remaining bytes into partPath starting at offset.
func (s *Syncer) downloadOnce(ctx context.Context, it MediaItem, partPath string, offset int64) error {
	resp, err := s.client.OpenFile(ctx, it.ID, offset)
	if err != nil {
		return retryErr("连接失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return fmt.Errorf("手机端找不到文件 %s", it.Name)
	default:
		return retryErr("HTTP %d", resp.StatusCode)
	}

	// Server ignored the Range header -> restart from zero.
	if offset > 0 && resp.StatusCode == http.StatusOK {
		offset = 0
	}

	flags := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 256*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			// Live progress so the UI moves smoothly even for big videos.
			atomic.AddInt64(&s.stats.LiveBytes, int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return retryErr("读取中断: %w", rerr)
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}

	expected := it.Size
	if expected > 0 {
		st, err := os.Stat(partPath)
		if err != nil {
			return err
		}
		if st.Size() != expected {
			return retryErr("文件大小不符: 期望 %d 实际 %d", expected, st.Size())
		}
	}
	return nil
}

var illegal = strings.NewReplacer(
	"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
	"\"", "_", "<", "_", ">", "_", "|", "_", "\x00", "",
)

// sanitizeName makes a single filename safe on Windows and Linux.
func sanitizeName(name string) string {
	name = illegal.Replace(name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	if name == "" {
		name = "unnamed"
	}
	// Windows reserved device names
	upper := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch upper {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}

func sanitizePathComponent(p string) string {
	if p == "" {
		return p
	}
	return sanitizeName(p)
}

// LatestMtime returns the max mtime among synced entries, for --since shortcuts.
func (m *Manifest) LatestMtime() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var max int64
	for _, e := range m.Entries {
		if e.Mtime > max {
			max = e.Mtime
		}
	}
	return max
}
