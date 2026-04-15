// Package audit appends one-line JSON events and rotates at 10MB per user decision.
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxSize = 10 * 1024 * 1024 // 10MB

type Logger struct {
	Path string
	mu   sync.Mutex
}

type Event struct {
	Time     time.Time `json:"time"`
	Command  string    `json:"command"`
	Property string    `json:"property,omitempty"`
	Target   string    `json:"target,omitempty"`
	Action   string    `json:"action"`
	OK       bool      `json:"ok"`
	Err      string    `json:"error,omitempty"`
}

func New(path string) *Logger { return &Logger{Path: path} }

func (l *Logger) Append(e Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	if err := l.maybeRotate(); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = f.Write(b)
	return err
}

func (l *Logger) maybeRotate() error {
	fi, err := os.Stat(l.Path)
	if err != nil || fi.Size() < MaxSize {
		return nil
	}
	rotated := l.Path + "." + time.Now().UTC().Format("20060102T150405")
	return os.Rename(l.Path, rotated)
}
