package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CacheDir mirrors CACHE_DIR: <backup_root>/cache.
func CacheDir() string {
	return filepath.Join(GetBackupRoot(), "cache")
}

type cacheEnvelope struct {
	Timestamp float64 `json:"timestamp"`
	Data      any     `json:"data"`
}

// ReadCache mirrors read_cache. Returns the stored data and true if the file
// exists, parses, and is within ttl; otherwise (nil, false). A zero or
// negative ttl always misses.
func ReadCache(path string, ttl time.Duration) (any, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var env cacheEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, false
	}
	age := time.Since(time.UnixMilli(int64(env.Timestamp * 1000)))
	if age < 0 || age >= ttl {
		return nil, false
	}
	return env.Data, true
}

// WriteCache mirrors write_cache: writes data to a cache file with a
// timestamp.
func WriteCache(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	env := cacheEnvelope{Timestamp: float64(time.Now().Unix()), Data: data}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}
