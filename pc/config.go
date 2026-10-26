package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Config is the persistent per-user client configuration.
//
// It holds one [Device] record per paired phone so several phones can sync to
// the same library root while keeping separate folders, tokens and manifests.
type Config struct {
	// Version of the config schema, for future migrations.
	Version int `json:"version,omitempty"`
	// OutDir is the library root. Each device gets OutDir/<device folder>.
	OutDir string `json:"outDir,omitempty"`
	// Devices lists every phone ever paired, most recently used first.
	Devices []*Device `json:"devices,omitempty"`
	// LastSync unix seconds of the most recent successful run.
	LastSync int64 `json:"lastSync,omitempty"`

	// --- legacy single-device fields (migrated on load) ---
	LastEndpoint string `json:"lastEndpoint,omitempty"`
	Token        string `json:"token,omitempty"`
	Device       string `json:"device,omitempty"`
}

// Device is one paired phone.
type Device struct {
	// ID is the phone's stable random id (8 hex chars), the primary key.
	ID string `json:"id"`
	// Name is the user-visible device name as reported by the phone.
	Name string `json:"name,omitempty"`
	// Folder is the per-device subdirectory under OutDir ("" = OutDir itself).
	Folder string `json:"folder,omitempty"`
	// Token is that phone's pairing token.
	Token string `json:"token,omitempty"`
	// Endpoint is the last working "ip:port".
	Endpoint string `json:"endpoint,omitempty"`
	// Model / Android are informational only.
	Model   string `json:"model,omitempty"`
	Android string `json:"android,omitempty"`
	// LastSync unix seconds for this device.
	LastSync int64 `json:"lastSync,omitempty"`
	// Added unix seconds.
	Added int64 `json:"added,omitempty"`
	// IncludeVideos remembers the per-device preference (default true).
	IncludeVideos *bool `json:"includeVideos,omitempty"`
}

// VideosEnabled reports whether videos should be synced for this device.
func (d *Device) VideosEnabled() bool {
	if d.IncludeVideos == nil {
		return true
	}
	return *d.IncludeVideos
}

// DisplayName is what we print in lists.
func (d *Device) DisplayName() string {
	name := d.Name
	if name == "" {
		name = d.ID
	}
	if d.Folder == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, d.Folder)
}

// ConfigPath returns the platform-appropriate config location.
func ConfigPath() string {
	if v := os.Getenv("PHOTOSYNC_CONFIG"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			base = "."
		}
		return filepath.Join(base, "PhotoSync", "config.json")
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "photosync", "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, ".config", "photosync", "config.json")
}

// LoadConfig reads the config file, migrating the legacy single-device format.
func LoadConfig() (*Config, error) {
	cfg := &Config{Version: 2}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, fmt.Errorf("配置文件损坏 %s: %w", ConfigPath(), err)
	}
	if cfg.Version == 0 {
		cfg.Version = 2
	}
	cfg.migrateLegacy()
	return cfg, nil
}

// migrateLegacy folds a v1 single-device config into the Devices list.
func (c *Config) migrateLegacy() {
	if c.Token == "" && c.LastEndpoint == "" && c.Device == "" {
		return
	}
	name := c.Device
	if name == "" {
		name = "Android"
	}
	id := deriveDeviceID(name, c.LastEndpoint)
	// Skip if we already have this device (running the migration twice).
	for _, d := range c.Devices {
		if d.ID == id {
			c.clearLegacy()
			return
		}
	}
	folder := sanitizeDeviceFolder(name)
	c.Devices = append(c.Devices, &Device{
		ID:       id,
		Name:     name,
		Folder:   folder,
		Token:    c.Token,
		Endpoint: c.LastEndpoint,
		LastSync: c.LastSync,
		Added:    time.Now().Unix(),
	})
	// The legacy library lived directly in OutDir; the photo files stay put and
	// we keep Folder=="" so the manifest is still found. Only new phones nest.
	c.Devices[len(c.Devices)-1].Folder = ""
	c.clearLegacy()
}

func (c *Config) clearLegacy() {
	c.Token = ""
	c.LastEndpoint = ""
	c.Device = ""
}

// Save writes the config atomically.
func (c *Config) Save() error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	c.Version = 2
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FindDevice returns the record whose ID matches, or nil.
func (c *Config) FindDevice(id string) *Device {
	for _, d := range c.Devices {
		if d.ID == id {
			return d
		}
	}
	return nil
}

// FindDeviceByEndpoint returns the record whose endpoint matches.
func (c *Config) FindDeviceByEndpoint(endpoint string) *Device {
	if endpoint == "" {
		return nil
	}
	for _, d := range c.Devices {
		if d.Endpoint == endpoint {
			return d
		}
	}
	return nil
}

// FindDeviceByName matches on the advertised device name (case-insensitive).
func (c *Config) FindDeviceByName(name string) *Device {
	for _, d := range c.Devices {
		if strings.EqualFold(d.Name, name) {
			return d
		}
	}
	return nil
}

// UpsertDevice records or refreshes a phone after a successful handshake.
func (c *Config) UpsertDevice(info DeviceInfo, folder string) *Device {
	id := info.DeviceID
	if id == "" {
		id = deriveDeviceID(info.Device, "")
	}
	d := c.FindDevice(id)
	if d == nil {
		d = &Device{ID: id, Added: time.Now().Unix()}
		c.Devices = append(c.Devices, d)
	}
	if info.Device != "" {
		d.Name = info.Device
	}
	if folder != "" {
		d.Folder = folder
	} else {
		// Prefer the folder the phone suggests; fall back to a sanitized name.
		d.Folder = sanitizeDeviceFolder(idOr(info.Folder, id))
	}
	if info.Model != "" {
		d.Model = info.Model
	}
	if info.Android != "" {
		d.Android = info.Android
	}
	if info.Port > 0 {
		if ip := info.BestIP(); ip != "" {
			d.Endpoint = fmt.Sprintf("%s:%d", ip, info.Port)
		}
	}
	// Keep most-recently-used first.
	sort.SliceStable(c.Devices, func(i, j int) bool {
		return c.Devices[i].LastSync > c.Devices[j].LastSync
	})
	return d
}

// DeviceDir returns the absolute destination directory for a device.
func (c *Config) DeviceDir(d *Device) string {
	root := c.OutDir
	if root == "" {
		root = DefaultOutDir()
	}
	if d == nil || d.Folder == "" {
		return root
	}
	return filepath.Join(root, d.Folder)
}

// RemoveDevice forgets a phone (its token and endpoint) without touching files.
func (c *Config) RemoveDevice(id string) bool {
	for i, d := range c.Devices {
		if d.ID == id {
			c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
			return true
		}
	}
	return false
}

// deriveDeviceID builds a deterministic id when the phone does not report one
// (older app builds, or a manual --host connection).
func deriveDeviceID(name, endpoint string) string {
	seed := name + "|" + endpoint
	if seed == "|" {
		seed = "android"
	}
	// FNV-1a 32-bit, hex encoded -> 8 chars, mirrors the phone's id shape.
	var h uint32 = 2166136261
	for i := 0; i < len(seed); i++ {
		h ^= uint32(seed[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

// sanitizeDeviceFolder makes a name safe as a single directory component.
func sanitizeDeviceFolder(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Android"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ', r == '/', r == '\\', r == ':', r == '*', r == '?',
			r == '"', r == '<', r == '>', r == '|':
			b.WriteRune('-')
		default:
			if r > 127 {
				b.WriteRune('-')
			} else {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Trim(b.String(), "-.")
	out = strings.TrimSpace(out)
	if out == "" {
		return "Android"
	}
	// Windows reserved names
	switch strings.ToUpper(out) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = "_" + out
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func idOr(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// uniqueFolder ensures a folder name does not collide with another device.
func (c *Config) uniqueFolder(want string, exceptID string) string {
	taken := map[string]bool{}
	for _, d := range c.Devices {
		if d.ID == exceptID {
			continue
		}
		if d.Folder != "" {
			taken[strings.ToLower(d.Folder)] = true
		}
	}
	if !taken[strings.ToLower(want)] {
		return want
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", want, i)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return want
}

// DefaultOutDir picks a sensible photo destination per platform.
func DefaultOutDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	if runtime.GOOS == "windows" {
		if pics := filepath.Join(home, "Pictures"); dirExists(pics) {
			return filepath.Join(pics, "Android")
		}
		return filepath.Join(home, "AndroidPhotos")
	}
	if pics := filepath.Join(home, "Pictures"); dirExists(pics) {
		return filepath.Join(home, "Pictures", "Android")
	}
	return filepath.Join(home, "AndroidPhotos")
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
