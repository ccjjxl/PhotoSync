package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manifest records what has already been transferred so incremental runs only
// move new or changed photos.
type Manifest struct {
	Version int              `json:"version"`
	Device  string           `json:"device,omitempty"`
	Updated string           `json:"updated,omitempty"`
	Entries map[string]Entry `json:"entries"`

	dir   string
	mu    sync.Mutex
	dirty bool
}

// Entry is one synced file.
type Entry struct {
	ID    FlexString `json:"id"`
	Size  int64      `json:"size"`
	Mtime int64      `json:"mtime"`
	Local string     `json:"local"` // path relative to the output directory
	At    string     `json:"at"`
}

const manifestName = ".photosync-manifest.json"

// OpenManifest loads (or creates) the sidecar manifest inside outDir.
func OpenManifest(outDir, device string) (*Manifest, error) {
	m := &Manifest{
		Version: 1,
		Device:  device,
		Entries: map[string]Entry{},
		dir:     outDir,
	}
	path := filepath.Join(outDir, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, m); err != nil {
			// Corrupt manifest: back it up and start fresh rather than refusing to run.
			_ = os.WriteFile(path+".corrupt", data, 0o644)
			m.Entries = map[string]Entry{}
		}
	}
	if m.Entries == nil {
		m.Entries = map[string]Entry{}
	}
	m.dir = outDir
	return m, nil
}

// Get returns the entry for a remote path.
func (m *Manifest) Get(remotePath string) (Entry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.Entries[remotePath]
	return e, ok
}

// Put records a successful transfer.
func (m *Manifest) Put(remotePath string, e Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.At = time.Now().Format(time.RFC3339)
	m.Entries[remotePath] = e
	m.dirty = true
}

// Len reports how many files are tracked.
func (m *Manifest) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Entries)
}

// Save writes the manifest atomically when it changed.
func (m *Manifest) Save(force bool) error {
	m.mu.Lock()
	if !m.dirty && !force {
		m.mu.Unlock()
		return nil
	}
	data, err := json.MarshalIndent(m, "", " ")
	m.Updated = time.Now().Format(time.RFC3339)
	m.dirty = false
	m.mu.Unlock()
	if err != nil {
		return err
	}
	path := filepath.Join(m.dir, manifestName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TotalBytes sums tracked file sizes.
func (m *Manifest) TotalBytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for _, e := range m.Entries {
		total += e.Size
	}
	return total
}

// SaveWithRetry retries the manifest write a few times, then reports.
func (m *Manifest) SaveWithRetry() error {
	var err error
	for i := 0; i < 3; i++ {
		if err = m.Save(true); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("写入清单失败: %w", err)
}
