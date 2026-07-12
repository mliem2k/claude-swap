package cswap

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")
	if err := WriteCache(path, map[string]any{"x": float64(1)}); err != nil {
		t.Fatal(err)
	}
	data, found := ReadCache(path, time.Minute)
	if !found {
		t.Fatal("expected cache hit")
	}
	m, ok := data.(map[string]any)
	if !ok || m["x"] != float64(1) {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func TestReadCacheExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")
	if err := WriteCache(path, "value"); err != nil {
		t.Fatal(err)
	}
	_, found := ReadCache(path, 0)
	if found {
		t.Fatal("a zero TTL should always be expired")
	}
}

func TestReadCacheMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	_, found := ReadCache(path, time.Hour)
	if found {
		t.Fatal("expected no cache for a missing file")
	}
}

func TestReadCacheCorruptIsTreatedAsMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry.json")
	mustWrite(t, path, "not json")
	_, found := ReadCache(path, time.Hour)
	if found {
		t.Fatal("corrupt cache file should read as a miss")
	}
}
