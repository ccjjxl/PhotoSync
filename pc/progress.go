package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
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
			if r.quiet || r.enabled {
				return
			}
			if r.multi {
				// Too noisy for several devices; show progress on completion only.
				return
			}
			fmt.Printf("↓ %s\n", item.Path)
			return

		case EventDone:
			if r.quiet || r.enabled {
				return
			}
			fmt.Printf("%s✓ %s (%s)\n", r.prefix(), item.Path, humanBytes(item.Size))
			return

		case EventFailed:
			r.clearLine()
			name := ""
			if item != nil {
				name = item.Path
			}
			fmt.Fprintf(os.Stdout, "\r\033[K%s✗ %s: %v\n", r.prefix(), name, err)
			return

		case EventDiscovered:
			if r.quiet {
				return
			}
			files := atomic.LoadInt64(&s.TotalFiles)
			bytes := atomic.LoadInt64(&s.TotalBytes)
			if r.enabled {
				r.clearLine()
			}
			if r.multi {
				fmt.Printf("%s→ %d 个待传输文件，共 %s\n", r.prefix(), files, humanBytes(bytes))
			} else {
				fmt.Printf("发现 %d 个待传输文件，共 %s\n", files, humanBytes(bytes))
			}
			return

		case EventFinished:
			r.finish(s)
			return
		}

		if r.enabled {
			r.draw(s)
		}
	}
}

func (r *renderer) clearLine() {
	if r.enabled {
		fmt.Fprint(os.Stdout, "\r\033[K")
	}
}

func (r *renderer) draw(s *Stats) {
	if time.Since(r.lastDraw) < 90*time.Millisecond {
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
	if r.finalised {
		return
	}
	r.finalised = true
	if r.enabled {
		r.clearLine()
	}
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
		fmt.Printf("%s%s 完成：%d 个文件 (%s)，跳过 %d，失败 %d，用时 %s\n",
			r.prefix(), icon, done-failed, humanBytes(bytes), skipped, failed, elapsed)
	} else {
		fmt.Printf("%s 完成：传输 %d 个文件 (%s)，跳过 %d 个，失败 %d 个，用时 %s，平均 %s/s\n",
			icon, done-failed, humanBytes(bytes), skipped, failed, elapsed, humanBytes(int64(speed)))
	}
	if failed > 0 {
		fmt.Printf("  有 %d 个文件失败，重新运行同样的命令会自动续传。\n", failed)
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
