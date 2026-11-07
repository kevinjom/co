package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBashPrintsOutputAndExitCode(t *testing.T) {
	set, _ := bashSet(t)
	got, err := runBash(t, set, "echo hello; echo err >&2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "exit: 0\nhello\nerr" {
		t.Fatalf("result %q", got)
	}

	got, err = runBash(t, set, "echo fail; exit 3")
	if err != nil {
		t.Fatal(err)
	}
	if got != "exit: 3\nfail" {
		t.Fatalf("result %q", got)
	}
}

func TestBashRejectsAnEmptyCommand(t *testing.T) {
	set, _ := bashSet(t)
	_, err := set.Run(context.Background(), "bash", map[string]any{})
	if err == nil {
		t.Fatal("expected an error")
	}
	_, err = set.Run(context.Background(), "bash", map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected an error for a blank command")
	}
}

func TestBashFileWrites(t *testing.T) {
	set, root := bashSet(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	got, err := runBash(t, set, "touch inside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "exit: 0" {
		t.Fatalf("result %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "inside.txt")); err != nil {
		t.Fatal(err)
	}

	got, err = runBash(t, set, `touch "$HOME/.co/ok.txt"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "exit: 0" {
		t.Fatalf("config write %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".co", "ok.txt")); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(home, "nope.txt")
	got, err = runBash(t, set, `touch "$HOME/nope.txt"`)
	if err != nil {
		t.Fatal(err)
	}
	assertWrite(t, got, outside, runtime.GOOS == "linux")

	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	got, err = runBash(t, set, "touch escape/nope.txt")
	if err != nil {
		t.Fatal(err)
	}
	assertWrite(t, got, filepath.Join(target, "nope.txt"), runtime.GOOS == "linux")

	secret := filepath.Join(home, "secret.txt")
	if err := os.WriteFile(secret, []byte("topsecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = runBash(t, set, `cat "$HOME/secret.txt"`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "exit: 0\ntopsecret" {
		t.Fatalf("read %q", got)
	}
}

func TestBashPointsCachesAtScratch(t *testing.T) {
	set, _ := bashSet(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", "/custom/modcache")
	scratch := filepath.Join(home, ".co", "bash")
	got, err := runBash(t, set, `printf '%s\n%s\n%s\n' "$GOCACHE" "$TMPDIR" "$GOMODCACHE"`)
	if err != nil {
		t.Fatal(err)
	}
	want := "exit: 0\n" + scratch + "\n" + scratch + "\n/custom/modcache"
	if got != want {
		t.Fatalf("env %q", got)
	}
	info, err := os.Stat(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestBashKeepsTheOutputTail(t *testing.T) {
	set, _ := bashSet(t)
	got, err := runBash(t, set, "head -c 40000 /dev/zero | tr '\\0' a; printf END")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "truncated: true\n") {
		t.Fatalf("not truncated: %q", got[:80])
	}
	if !strings.HasSuffix(got, "END") {
		t.Fatalf("tail %q", got[len(got)-16:])
	}
	body := got[strings.Index(got, "\n")+1:]
	body = strings.TrimPrefix(body, "truncated: true\n")
	if len(body) > maxBashOutput {
		t.Fatalf("kept %d bytes", len(body))
	}
}

func TestBashCancelKillsTheGroup(t *testing.T) {
	set, root := bashSet(t)
	set.bashTimeout = 15 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := set.Run(ctx, "bash", map[string]any{
			"command": "sleep 30 & echo $! > child.pid; wait",
		})
		done <- err
	}()

	pidPath := filepath.Join(root, "child.pid")
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for {
		body, err := os.ReadFile(pidPath)
		if err == nil && strings.TrimSpace(string(body)) != "" {
			pid, err = strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child pid was not written")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not return")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("err %v", runErr)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("pid %d still running", pid)
	}
}

func TestBashTimeoutKillsTheCommand(t *testing.T) {
	set, _ := bashSet(t)
	set.bashTimeout = 200 * time.Millisecond
	started := time.Now()
	_, err := runBash(t, set, "sleep 30")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("took %s", time.Since(started))
	}
}

func assertWrite(t *testing.T, got, path string, denied bool) {
	t.Helper()
	_, err := os.Stat(path)
	if denied {
		if !strings.HasPrefix(got, "exit: ") || strings.HasPrefix(got, "exit: 0") {
			t.Fatalf("write %q", got)
		}
		if !os.IsNotExist(err) {
			t.Fatalf("file err=%v", err)
		}
		return
	}
	if got != "exit: 0" {
		t.Fatalf("write %q", got)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func bashSet(t *testing.T) (*Set, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	return New(root), root
}

func runBash(t *testing.T, set *Set, command string) (string, error) {
	t.Helper()
	return set.Run(context.Background(), "bash", map[string]any{"command": command})
}
