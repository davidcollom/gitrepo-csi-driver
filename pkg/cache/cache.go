package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Config struct {
	RootDir string
	MaxSize int64
	MaxAge  time.Duration
}

type Manager struct {
	cfg Config
}

func New(cfg Config) (*Manager, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Errorf("cache root dir is required")
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10 * 1024 * 1024 * 1024
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 24 * time.Hour
	}
	if err := os.MkdirAll(cfg.RootDir, 0o750); err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg}, nil
}

func (m *Manager) Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Manager) PathForKey(key string) string {
	return filepath.Join(m.cfg.RootDir, key)
}

func (m *Manager) Evict() error {
	type item struct {
		path    string
		size    int64
		modTime time.Time
	}

	now := time.Now()
	var items []item
	var total int64
	entries, err := os.ReadDir(m.cfg.RootDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(m.cfg.RootDir, e.Name())
		i, err := dirInfo(p)
		if err != nil {
			continue
		}
		total += i.size
		items = append(items, i)
		if now.Sub(i.modTime) > m.cfg.MaxAge {
			_ = os.RemoveAll(i.path)
			total -= i.size
		}
	}

	if total <= m.cfg.MaxSize {
		return nil
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})

	for _, i := range items {
		if total <= m.cfg.MaxSize {
			break
		}
		_ = os.RemoveAll(i.path)
		total -= i.size
	}

	return nil
}

func dirInfo(path string) (struct {
	path    string
	size    int64
	modTime time.Time
}, error) {
	var out struct {
		path    string
		size    int64
		modTime time.Time
	}
	out.path = path
	out.modTime = time.Now()
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out.size += info.Size()
		if info.ModTime().Before(out.modTime) {
			out.modTime = info.ModTime()
		}
		return nil
	})
	return out, err
}
