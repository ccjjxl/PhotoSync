package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// iPhones can hold several photos with the same album + filename (bursts,
// re-imports). Planning must assign each a distinct destination, otherwise one
// overwrites the other and the manifest records two entries for one file —
// which makes every subsequent run re-download the same batch.
func TestPlanAssignsUniqueDestinations(t *testing.T) {
	dir := t.TempDir()
	s := &Syncer{opts: Options{OutDir: dir}, man: mustManifest(t, dir)}

	items := []MediaItem{
		{ID: "AAAA/1", Name: "IMG_0001.JPG", Path: "DCIM/Camera/IMG_0001.JPG"},
		{ID: "BBBB/2", Name: "IMG_0001.JPG", Path: "DCIM/Camera/IMG_0001.JPG"},
		{ID: "CCCC/3", Name: "IMG_0001.JPG", Path: "DCIM/Camera/IMG_0001.JPG"},
		{ID: "DDDD/4", Name: "IMG_0002.JPG", Path: "DCIM/Camera/IMG_0002.JPG"},
	}

	destinations := planDestinations(t, s, items)

	seen := map[string]string{}
	for i, dst := range destinations {
		key := strings.ToLower(filepath.ToSlash(dst))
		if prev, dup := seen[key]; dup {
			t.Fatalf("第 %d 项与 %s 撞路径: %s", i, prev, dst)
		}
		seen[key] = string(items[i].ID)
	}
	if len(seen) != len(items) {
		t.Fatalf("期望 %d 个唯一路径，实际 %d", len(items), len(seen))
	}
	// Unrelated files must keep their natural name.
	if filepath.Base(destinations[3]) != "IMG_0002.JPG" {
		t.Fatalf("无冲突文件被改名: %s", destinations[3])
	}
}

// uniquePath must be stable and must never touch an existing file.
func TestUniquePathStableAndNonDestructive(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "IMG_0001.JPG")
	if err := os.WriteFile(base, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	used := map[string]string{strings.ToLower(filepath.ToSlash(base)): "AAAA/1"}
	first := uniquePath(base, "BBBB/2", used)
	second := uniquePath(base, "BBBB/2", used)
	if first != second {
		t.Fatalf("uniquePath 不稳定: %s vs %s", first, second)
	}
	if first == base {
		t.Fatal("uniquePath 返回了已被占用的路径")
	}
	if filepath.Ext(first) != ".JPG" {
		t.Fatalf("扩展名丢失: %s", first)
	}

	// Filling the first candidate must push it to the next one.
	if err := os.WriteFile(first, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	third := uniquePath(base, "BBBB/2", map[string]string{})
	if third == first || third == base {
		t.Fatalf("uniquePath 没有避开磁盘上的文件: %s", third)
	}

	// The original content must be untouched.
	data, err := os.ReadFile(base)
	if err != nil || string(data) != "original" {
		t.Fatalf("原有文件被破坏: %q %v", data, err)
	}
}

// --- helpers ---------------------------------------------------------------

func mustManifest(t *testing.T, dir string) *Manifest {
	t.Helper()
	man, err := OpenManifest(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	return man
}

// planDestinations mirrors the planning loop's path assignment in isolation.
func planDestinations(t *testing.T, s *Syncer, items []MediaItem) []string {
	t.Helper()
	used := s.reserveExistingNames()
	out := make([]string, len(items))
	for i, it := range items {
		dst := s.localPath(it)
		if other, clash := used[strings.ToLower(toSlash(dst))]; clash && other != string(it.ID) {
			dst = uniquePath(dst, string(it.ID), used)
		}
		used[strings.ToLower(toSlash(dst))] = string(it.ID)
		out[i] = dst
	}
	return out
}
