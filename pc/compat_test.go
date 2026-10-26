package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Manifests written before the "#id" key suffix are keyed by remote path.
// Reading them with the new key must still hit, otherwise every existing user
// would re-download their whole library after upgrading.
func TestManifestLegacyKeyCompat(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"version": 1,
		"device":  "old",
		"entries": map[string]any{
			"DCIM/Camera/IMG_0001.JPG": map[string]any{
				"id": "123", "size": 7, "mtime": 1000,
				"local": "DCIM/Camera/IMG_0001.JPG",
			},
		},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, ".photosync-manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	man, err := OpenManifest(dir, "old")
	if err != nil {
		t.Fatal(err)
	}

	// New-style key must resolve the legacy entry.
	entry, ok := man.Get("DCIM/Camera/IMG_0001.JPG#123")
	if !ok {
		t.Fatal("旧清单条目无法通过新 key 命中 —— 升级会触发全量重传")
	}
	if entry.Size != 7 {
		t.Fatalf("读到错误条目: %+v", entry)
	}

	// New-style keys are still found exactly.
	man.Put("DCIM/Camera/IMG_0001.JPG#999", Entry{ID: "999", Size: 42})
	if e, ok := man.Get("DCIM/Camera/IMG_0001.JPG#999"); !ok || e.Size != 42 {
		t.Fatal("新 key 无法读回")
	}
	// The legacy entry must not be shadowed by that.
	if e, ok := man.Get("DCIM/Camera/IMG_0001.JPG#123"); !ok || e.Size != 7 {
		t.Fatal("新增条目污染了旧条目")
	}
}

// Two same-named photos must occupy two manifest slots.
func TestManifestDistinguishesDuplicates(t *testing.T) {
	dir := t.TempDir()
	man := mustManifest(t, dir)

	man.Put("DCIM/Camera/IMG_0001.JPG#AAAA", Entry{ID: "AAAA", Size: 10, Local: "a.JPG"})
	man.Put("DCIM/Camera/IMG_0001.JPG#BBBB", Entry{ID: "BBBB", Size: 20, Local: "b.JPG"})

	if man.Len() != 2 {
		t.Fatalf("同名照片应占两个槽位，实际 %d", man.Len())
	}
	if e, _ := man.Get("DCIM/Camera/IMG_0001.JPG#AAAA"); e.Size != 10 {
		t.Fatalf("AAAA 条目被覆盖: %+v", e)
	}
	if e, _ := man.Get("DCIM/Camera/IMG_0001.JPG#BBBB"); e.Size != 20 {
		t.Fatalf("BBBB 条目被覆盖: %+v", e)
	}
}
