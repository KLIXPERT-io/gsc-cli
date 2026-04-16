// Package cache provides a flat-file TTL cache per FR-2.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Entry struct {
	CachedAt time.Time       `json:"cached_at"`
	TTL      time.Duration   `json:"ttl"`
	Payload  json.RawMessage `json:"payload"`
}

type Store struct {
	Dir        string
	DefaultTTL time.Duration
	HintWriter func(msg string) // called once when the cache dir is first created
	hinted     bool
}

// New returns a Store with the given directory.
func New(dir string, defaultTTL time.Duration) *Store {
	if defaultTTL <= 0 {
		defaultTTL = 15 * time.Minute
	}
	return &Store{Dir: dir, DefaultTTL: defaultTTL}
}

// Key builds a cache key from command path + normalized args + property + identity.
func Key(cmdPath string, args []string, property, identity string) string {
	sorted := append([]string(nil), args...)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(cmdPath))
	h.Write([]byte{0})
	for _, a := range sorted {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	h.Write([]byte(property))
	h.Write([]byte{0})
	h.Write([]byte(identity))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) path(key string) string {
	return filepath.Join(s.Dir, key[:2], key[2:]+".json")
}

// Get returns the entry if present and unexpired. (nil, nil) if miss.
func (s *Store) Get(key string) (*Entry, error) {
	p := s.path(key)
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	if time.Since(e.CachedAt) > e.TTL {
		_ = os.Remove(p)
		return nil, nil
	}
	return &e, nil
}

// Put writes an entry with the given TTL (0 = default).
func (s *Store) Put(key string, payload json.RawMessage, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = s.DefaultTTL
	}
	p := s.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if !s.hinted && s.HintWriter != nil {
		if _, err := os.Stat(filepath.Join(s.Dir, ".hinted")); errors.Is(err, os.ErrNotExist) {
			s.HintWriter("cache dir created at " + s.Dir)
			_ = os.WriteFile(filepath.Join(s.Dir, ".hinted"), []byte("1"), 0o600)
			s.hinted = true
		}
	}
	e := Entry{CachedAt: time.Now().UTC(), TTL: ttl, Payload: payload}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Invalidate removes a specific key.
func (s *Store) Invalidate(key string) error {
	err := os.Remove(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// InvalidatePrefix removes all entries whose command prefix matches.
// Since keys are hashed, we clear the whole cache dir as a conservative fallback.
func (s *Store) Clear() error {
	return os.RemoveAll(s.Dir)
}

// Remaining returns time until expiry.
func (e *Entry) Remaining() time.Duration {
	return time.Until(e.CachedAt.Add(e.TTL))
}
