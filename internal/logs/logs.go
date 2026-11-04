// Package logs writes the process log under ~/.co/logs.
// Each local day is one file, named co-YYYY-MM-DD.log.
// Files from days outside the retention window are removed.
package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	filePrefix = "co-"
	fileSuffix = ".log"
	dayLayout  = "2006-01-02"
	dirPerm    = 0o700
	filePerm   = 0o600
)

// Dir is the log directory for home, ~/.co/logs.
func Dir(home string) string {
	return filepath.Join(home, ".co", "logs")
}

// File appends log lines for the current local day.
// The file is created on the first Write.
type File struct {
	dir    string
	retain int
	now    func() time.Time

	mu     sync.Mutex
	file   *os.File
	day    string
	closed bool
}

// Open prepares ~/.co/logs and removes daily files outside retainDays.
// retainDays includes today. A file is created on the first Write.
func Open(home string, retainDays int) (*File, error) {
	return openAt(home, retainDays, time.Now)
}

func openAt(home string, retainDays int, now func() time.Time) (*File, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("opening log: home directory is empty")
	}
	if retainDays < 1 {
		return nil, fmt.Errorf("opening log: retention must be at least 1 day")
	}
	f := &File{
		dir:    Dir(home),
		retain: retainDays,
		now:    now,
	}
	if err := os.MkdirAll(f.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	if err := os.Chmod(f.dir, dirPerm); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	if err := f.prune(now()); err != nil {
		return nil, err
	}
	return f, nil
}

// Write appends p to the log file for the current local day.
// A day change closes the previous file, opens the new one, and prunes again.
func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, fmt.Errorf("log file is closed")
	}
	now := f.now()
	if f.file == nil || now.Format(dayLayout) != f.day {
		if err := f.rotate(now); err != nil {
			return 0, err
		}
	}
	n, err := f.file.Write(p)
	if err != nil {
		return n, fmt.Errorf("writing log file: %w", err)
	}
	return n, nil
}

// Close closes the current log file. A later Close is a no-op.
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return f.closeFile()
}

func (f *File) rotate(now time.Time) error {
	if err := f.closeFile(); err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, dirPerm); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	path := filepath.Join(f.dir, filename(now))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	if err := file.Chmod(filePerm); err != nil {
		_ = file.Close()
		return fmt.Errorf("opening log file: %w", err)
	}
	f.file = file
	f.day = now.Format(dayLayout)
	return f.prune(now)
}

func (f *File) closeFile() error {
	if f.file == nil {
		return nil
	}
	err := f.file.Close()
	f.file = nil
	f.day = ""
	if err != nil {
		return fmt.Errorf("closing log file: %w", err)
	}
	return nil
}

func (f *File) prune(now time.Time) error {
	cutoff := dateOnly(now).AddDate(0, 0, -(f.retain - 1))
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return fmt.Errorf("reading log directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		day, ok := parseDay(entry.Name(), now.Location())
		if !ok || !day.Before(cutoff) {
			continue
		}
		path := filepath.Join(f.dir, entry.Name())
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing log file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func filename(day time.Time) string {
	return filePrefix + day.Format(dayLayout) + fileSuffix
}

func parseDay(name string, loc *time.Location) (time.Time, bool) {
	rest, ok := strings.CutPrefix(name, filePrefix)
	if !ok {
		return time.Time{}, false
	}
	raw, ok := strings.CutSuffix(rest, fileSuffix)
	if !ok {
		return time.Time{}, false
	}
	day, err := time.ParseInLocation(dayLayout, raw, loc)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

func dateOnly(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}
