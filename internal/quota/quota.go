// Package quota tracks daily API usage and rolling rate windows per FR-7.
package quota

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Default limits from the PRD.
const (
	URLInspectionDailyLimit = 2000
	SearchAnalyticsQPM      = 1200
)

type Counters struct {
	Date             string    `json:"date"` // YYYY-MM-DD in America/Los_Angeles
	URLInspection    int       `json:"url_inspection"`
	SearchAnalytics  int       `json:"search_analytics"` // all-time since reset (info only)
	Other            int       `json:"other"`
	LastWarnedAt     int       `json:"last_warned_at"` // last url_inspection count at which we warned
	// RecentSA is not persisted across restarts; FR acceptable best-effort.
}

type Store struct {
	Path string
	mu   sync.Mutex
	// in-memory rolling window for search analytics
	saEvents []time.Time
	// for quota warning emission
	WarnFn func(msg string)
}

var laLoc *time.Location

func init() {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		loc = time.FixedZone("PST", -8*3600)
	}
	laLoc = loc
}

func today() string { return time.Now().In(laLoc).Format("2006-01-02") }

func New(path string) *Store { return &Store{Path: path} }

// withLock opens the file (creating if missing), applies an exclusive flock,
// reads current state, runs fn which may mutate Counters, writes, unlocks.
func (s *Store) withLock(fn func(c *Counters) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	var c Counters
	b, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &c); err != nil {
			// corrupt — reset
			c = Counters{}
		}
	}
	// Reset on date rollover
	if c.Date != today() {
		c = Counters{Date: today()}
	}
	if err := fn(&c); err != nil {
		return err
	}
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err = f.Write(out)
	return err
}

// Load returns a snapshot (read-only, still rolls the date if stale).
func (s *Store) Load() (*Counters, error) {
	var out Counters
	err := s.withLock(func(c *Counters) error { out = *c; return nil })
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Bump increments a counter and returns the post-increment value.
// Supported categories: "url_inspection", "search_analytics", "other".
// Emits warnings for url_inspection and enforces hard stop at the daily limit.
func (s *Store) Bump(category string, n int) error {
	return s.withLock(func(c *Counters) error {
		switch category {
		case "url_inspection":
			if c.URLInspection+n > URLInspectionDailyLimit {
				return errQuotaExceeded(URLInspectionDailyLimit)
			}
			c.URLInspection += n
			s.maybeWarn(c)
		case "search_analytics":
			c.SearchAnalytics += n
		case "other":
			c.Other += n
		default:
			return fmt.Errorf("unknown quota category: %s", category)
		}
		return nil
	})
}

// BumpSA records search-analytics events for rolling-QPM tracking (best-effort, in-memory).
func (s *Store) BumpSA() error {
	s.mu.Lock()
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)
	kept := s.saEvents[:0]
	for _, t := range s.saEvents {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	s.saEvents = kept
	rate := len(kept)
	s.mu.Unlock()
	if rate > SearchAnalyticsQPM {
		return errRateLimited()
	}
	return s.Bump("search_analytics", 1)
}

func (s *Store) maybeWarn(c *Counters) {
	if s.WarnFn == nil {
		return
	}
	thresholds := []int{1000, 1500}
	for _, t := range thresholds {
		if c.URLInspection >= t && c.LastWarnedAt < t {
			s.WarnFn(fmt.Sprintf("url_inspection quota: %d/%d used", c.URLInspection, URLInspectionDailyLimit))
			c.LastWarnedAt = t
		}
	}
	// After 1500: every +100
	if c.URLInspection >= 1600 {
		step := (c.URLInspection / 100) * 100
		if step > c.LastWarnedAt {
			s.WarnFn(fmt.Sprintf("url_inspection quota: %d/%d used", c.URLInspection, URLInspectionDailyLimit))
			c.LastWarnedAt = step
		}
	}
}

// Sentinel errors resolved by the caller to structured errs.E values.
var (
	ErrQuotaExceeded = errors.New("quota_exceeded")
	ErrRateLimited   = errors.New("rate_limited")
)

func errQuotaExceeded(limit int) error {
	return fmt.Errorf("%w: daily limit %d reached", ErrQuotaExceeded, limit)
}
func errRateLimited() error { return fmt.Errorf("%w: search analytics QPM", ErrRateLimited) }
