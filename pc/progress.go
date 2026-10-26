package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// Minimum gap between event-driven redraws. The ticker uses its own,
	// slower cadence so large files still show movement.
	drawInterval = 90 * time.Millisecond
	tickInterval = 250 * time.Millisecond
)

// renderer draws a live single-line progress bar for one device, or prefixed
// per-file log lines when several devices sync in parallel (multiple
// concurrent bars would garble the terminal).
type renderer struct {
	enabled   bool // live bar allowed
	quiet     bool
	label     string // device name, used as a prefix in multi mode
	multi     bool   // more than one device in this run
	lastDraw  time.Time
	started   time.Time
	finalised bool

	// mu serialises log lines, bar redraws (including the ticker) and finish.
	mu         sync.Mutex
	stats      *Stats
	tickerStop chan struct{}
}

func newRenderer(quiet, verbose bool, label string, multi bool) *renderer {
	isTTY := false
	if st, err := os.Stdout.Stat(); err == nil {
		isTTY = st.Mode()&os.ModeCharDevice != 0
	}
	return &renderer{
		// A live bar only makes sense when there is exactly one device.
		enabled: isTTY && !quiet && !multi,
		quiet:   quiet,
		label:   label,
		multi:   multi,
		started: time.Now(),
	}
}

// prefix renders "[Pixel]" in multi-device mode, empty otherwise.
func (r *renderer) prefix() string {
	if !r.multi || r.label == "" {
		return ""
	}
	return "[" + r.label + "] "
}

// progress returns the Progress callback used by the Syncer.
func (r *renderer) progress() Progress {
	return func(s *Stats, ev Event, item *MediaItem, err error, dst string) {
		switch ev {
		case EventStart:
			if r.quiet {
				return
			}
			if r.enabled {
				r.draw(s)
				return
			}
			if r.multi {
				// Too noisy for several devices; show progress on completion only.
				return
			}
			r.line("↓ %s", item.Path)

		case EventDone:
			if r.quiet {
				return
			}
			if r.enabled {
				r.draw(s)
				return
			}
			r.line("%s✓ %s (%s)", r.prefix(), item.Path, humanBytes(item.Size))

		case EventFailed:
			name := ""
			if item != nil {
				name = item.Path
			}
			r.line("%s✗ %s: %v", r.prefix(), name, err)

		case EventDiscovered:
			if r.quiet {
				return
			}
			r.startTicker(s)
			files := atomic.LoadInt64(&s.TotalFiles)
			bytes := atomic.LoadInt64(&s.TotalBytes)
			if r.multi {
				r.line("%s→ %d 个待传输文件，共 %s", r.prefix(), files, humanBytes(bytes))
			} else {
				r.line("发现 %d 个待传输文件，共 %s", files, humanBytes(bytes))
			}

		case EventFinished:
			r.finish(s)
		}
	}
}

// line prints a normal log line after clearing any live bar. The mutex keeps it
// from interleaving with the ticker or other workers.
func (r *renderer) line(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lineLocked(format, args...)
}

func (r *renderer) lineLocked(format string, args ...any) {
	if r.enabled {
		fmt.Fprint(os.Stdout, "\r\033[K")
	}
	fmt.Fprintf(os.Stdout, format+"\n", args...)
	// Redraw as soon as the next event arrives instead of waiting for the tick.
	r.lastDraw = time.Time{}
}

// startTicker redraws on a fixed cadence while a live bar is active, so GB-sized
// files do not freeze the speed/ETA display between EventStart and EventDone.
func (r *renderer) startTicker(s *Stats) {
	if !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finalised || r.tickerStop != nil {
		return
	}
	r.stats = s
	stop := make(chan struct{})
	r.tickerStop = stop
	go func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.tick()
			case <-stop:
				return
			}
		}
	}()
}

func (r *renderer) tick() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stats == nil {
		return
	}
	r.drawLocked(r.stats)
}

func (r *renderer) stopTickerLocked() {
	if r.tickerStop == nil {
		return
	}
	close(r.tickerStop)
	r.tickerStop = nil
	r.stats = nil
}

func (r *renderer) draw(s *Stats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drawLocked(s)
}

func (r *renderer) drawLocked(s *Stats) {
	if !r.enabled || r.finalised {
		return
	}
	if time.Since(r.lastDraw) < drawInterval {
		return
	}
	r.lastDraw = time.Now()

	done := atomic.LoadInt64(&s.DoneFiles)
	live := atomic.LoadInt64(&s.LiveBytes)
	totalFiles := atomic.LoadInt64(&s.TotalFiles)
	totalBytes := atomic.LoadInt64(&s.TotalBytes)

	pct := 0
	if totalBytes > 0 {
		pct = int(live * 100 / totalBytes)
		if pct > 100 {
			pct = 100
		}
	} else if totalFiles > 0 {
		pct = int(done * 100 / totalFiles)
	}

	const width = 28
	filled := pct * width / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	active := ""
	if files := s.snapshotFiles(); len(files) > 0 {
		name := files[len(files)-1]
		if len(name) > 28 {
			name = "…" + name[len(name)-27:]
		}
		active = "  " + name
	}

	fmt.Fprintf(os.Stdout, "\r\033[K[%s] %3d%%  %s / %s  %d/%d  %s/s  ETA %s%s",
		bar, pct,
		humanBytes(live), humanBytes(totalBytes),
		done, totalFiles,
		humanBytes(int64(s.Speed())),
		humanETA(s.ETA()),
		dim(active),
	)
}

func (r *renderer) finish(s *Stats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finalised {
		return
	}
	r.finalised = true
	r.stopTickerLocked()
	if r.quiet {
		return
	}

	elapsed := time.Since(r.started).Round(time.Millisecond)
	done := atomic.LoadInt64(&s.DoneFiles)
	failed := atomic.LoadInt64(&s.Failed)
	skipped := atomic.LoadInt64(&s.Skipped)
	bytes := atomic.LoadInt64(&s.DoneBytes)

	speed := float64(bytes) / elapsed.Seconds()
	icon := "✓"
	if failed > 0 {
		icon = "!"
	}
	if r.multi {
		r.lineLocked("%s%s 完成：%d 个文件 (%s)，跳过 %d，失败 %d，用时 %s",
			r.prefix(), icon, done-failed, humanBytes(bytes), skipped, failed, elapsed)
	} else {
		r.lineLocked("%s 完成：传输 %d 个文件 (%s)，跳过 %d 个，失败 %d 个，用时 %s，平均 %s/s",
			icon, done-failed, humanBytes(bytes), skipped, failed, elapsed, humanBytes(int64(speed)))
	}
	if failed > 0 {
		r.lineLocked("  有 %d 个文件失败，重新运行同样的命令会自动续传。", failed)
	}
}

func dim(s string) string {
	if s == "" || os.Getenv("NO_COLOR") != "" {
		return s
	}
	return "\033[2m" + s + "\033[0m"
}

// humanBytes formats a byte count compactly.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.2f TB", float64(b)/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func humanETA(d time.Duration) string {
	if d <= 0 {
		return "--"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// installSignalHandler maps SIGINT/SIGTERM to context cancellation.
func installSignalHandler(cancel func()) func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			fmt.Fprintln(os.Stderr, "\n正在结束，已保存进度…")
			cancel()
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
			}
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
