package logs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenRejectsRetention(t *testing.T) {
	_, err := Open(t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "retention") {
		t.Fatalf("err = %v", err)
	}
}

func TestDailyFileAndPrune(t *testing.T) {
	home := t.TempDir()
	current := time.Date(2026, 11, 1, 15, 4, 5, 0, time.Local)
	f, err := openAt(home, 3, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	dir := Dir(home)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", info.Mode().Perm())
	}

	kept := filepath.Join(dir, filename(current.AddDate(0, 0, -2)))
	dropped := filepath.Join(dir, filename(current.AddDate(0, 0, -3)))
	other := filepath.Join(dir, "notes.txt")
	odd := filepath.Join(dir, "co-not-a-date.log")
	for _, path := range []string{kept, dropped, other, odd} {
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("started", "day", "one")
	if _, err := os.Stat(dropped); !os.IsNotExist(err) {
		t.Fatalf("old log stat %v", err)
	}
	for _, path := range []string{kept, other, odd} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	today := filepath.Join(dir, filename(current))
	info, err = os.Stat(today)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %o", info.Mode().Perm())
	}
	body, err := os.ReadFile(today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "started") || !strings.Contains(string(body), "day=one") {
		t.Fatalf("log %q", body)
	}

	current = current.AddDate(0, 0, 1)
	logger.Info("next")
	next := filepath.Join(dir, filename(current))
	body, err = os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "next") {
		t.Fatalf("next log %q", body)
	}
	body, err = os.ReadFile(today)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "next") {
		t.Fatalf("previous day gained %q", body)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Fatalf("boundary log after rollover: %v", err)
	}

	current = current.AddDate(0, 0, 2)
	logger.Info("later")
	if _, err := os.Stat(today); !os.IsNotExist(err) {
		t.Fatalf("first day stat %v", err)
	}
	if _, err := os.Stat(next); err != nil {
		t.Fatalf("day still inside the window: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filename(current))); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppendSameDay(t *testing.T) {
	home := t.TempDir()
	current := time.Date(2026, 11, 4, 8, 0, 0, 0, time.Local)
	f, err := openAt(home, 3, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("two\n")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(Dir(home), filename(current)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "one\ntwo\n" {
		t.Fatalf("log %q", body)
	}
}
