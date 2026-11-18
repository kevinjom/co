package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestUpdatePrintsAlreadyUpToDate(t *testing.T) {
	for _, current := range []string{"0.1.0", "v0.1.0"} {
		t.Run(current, func(t *testing.T) {
			prepare(t)
			dest := writeFakeBinary(t, []byte("old"))
			pinUpdate(t, current, dest)
			hits := serveReleases(t, releaseSpec{tag: "v0.1.0", asset: elfBinary()})

			stdout, stderr, err := startCLI(t, "", "update")
			if err != nil {
				t.Fatal(err)
			}
			if stdout != "already up to date\n" {
				t.Fatalf("stdout %q", stdout)
			}
			if strings.Contains(stderr, "Usage:") {
				t.Fatalf("usage leaked %q", stderr)
			}
			if hits.downloads() != 0 {
				t.Fatalf("downloads %d", hits.downloads())
			}
			if got := mustRead(t, dest); string(got) != "old" {
				t.Fatalf("binary %q", got)
			}
		})
	}
}

func TestUpdateReplacesTheBinary(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	if err := os.Chmod(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	pinUpdate(t, "0.1.0", dest)
	asset := elfBinary()
	hits := serveReleases(t, releaseSpec{tag: "v0.2.0", asset: asset})

	stdout, _, err := startCLI(t, "", "update")
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolvedPath(t, dest)
	if stdout != fmt.Sprintf("updated %s to v0.2.0\n", resolved) {
		t.Fatalf("stdout %q", stdout)
	}
	if hits.downloads() != 1 {
		t.Fatalf("downloads %d", hits.downloads())
	}
	wantPath := "/" + path.Join(
		releaseRepo,
		"releases",
		"download",
		"v0.2.0",
		fmt.Sprintf("co_0.2.0_%s_%s", runtime.GOOS, runtime.GOARCH),
	)
	if hits.assetPath() != wantPath {
		t.Fatalf("asset %s", hits.assetPath())
	}
	if got := mustRead(t, dest); string(got) != string(asset) {
		t.Fatalf("binary %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestUpdateFromGoRunReplacesTheInstalledBinary(t *testing.T) {
	prepare(t)
	runner := writeGoRunBinary(t)
	installed := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "dev", runner)
	updateLookPath = func(name string) (string, error) {
		if name != "co" {
			return "", fmt.Errorf("name %s", name)
		}
		return installed, nil
	}
	updateInstalledVersion = func(_ context.Context, file string) (string, error) {
		if file != resolvedPath(t, installed) {
			return "", fmt.Errorf("version of %s", file)
		}
		return "0.1.2", nil
	}
	asset := elfBinary()
	hits := serveReleases(t, releaseSpec{tag: "v0.1.5", asset: asset})

	stdout, _, err := startCLI(t, "", "update")
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolvedPath(t, installed)
	if stdout != fmt.Sprintf("updated %s to v0.1.5\n", resolved) {
		t.Fatalf("stdout %q", stdout)
	}
	if hits.downloads() != 1 {
		t.Fatalf("downloads %d", hits.downloads())
	}
	if got := mustRead(t, installed); string(got) != string(asset) {
		t.Fatalf("installed %q", got)
	}
	if got := mustRead(t, runner); string(got) != "runner" {
		t.Fatalf("go run binary %q", got)
	}
}

func TestUpdateFromGoRunPrintsAlreadyUpToDate(t *testing.T) {
	prepare(t)
	runner := writeGoRunBinary(t)
	installed := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "dev", runner)
	updateLookPath = func(string) (string, error) { return installed, nil }
	updateInstalledVersion = func(context.Context, string) (string, error) {
		return "0.1.5", nil
	}
	hits := serveReleases(t, releaseSpec{tag: "v0.1.5", asset: elfBinary()})

	stdout, _, err := startCLI(t, "", "update")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "already up to date\n" {
		t.Fatalf("stdout %q", stdout)
	}
	if hits.downloads() != 0 {
		t.Fatalf("downloads %d", hits.downloads())
	}
	if got := mustRead(t, installed); string(got) != "old" {
		t.Fatalf("installed %q", got)
	}
	if got := mustRead(t, runner); string(got) != "runner" {
		t.Fatalf("go run binary %q", got)
	}
}

func TestUpdateFromGoRunReportsNoInstalledBinary(t *testing.T) {
	tests := []struct {
		name string
		find func(runner string) (string, error)
	}{
		{
			name: "missing",
			find: func(string) (string, error) { return "", exec.ErrNotFound },
		},
		{
			name: "cache",
			find: func(runner string) (string, error) { return runner, nil },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepare(t)
			runner := writeGoRunBinary(t)
			pinUpdate(t, "dev", runner)
			updateLookPath = func(string) (string, error) { return tt.find(runner) }
			hits := serveReleases(t, releaseSpec{tag: "v0.1.5", asset: elfBinary()})

			_, _, err := startCLI(t, "", "update")
			if err == nil || !strings.Contains(err.Error(), "no installed co on PATH") {
				t.Fatalf("err %v", err)
			}
			if hits.downloads() != 0 {
				t.Fatalf("downloads %d", hits.downloads())
			}
			if got := mustRead(t, runner); string(got) != "runner" {
				t.Fatalf("go run binary %q", got)
			}
		})
	}
}

func TestUpdateReplacesADevBuild(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "dev", dest)
	hits := serveReleases(t, releaseSpec{tag: "v0.2.0", asset: elfBinary()})

	stdout, _, err := startCLI(t, "", "update")
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolvedPath(t, dest)
	if stdout != fmt.Sprintf("updated %s to v0.2.0\n", resolved) {
		t.Fatalf("stdout %q", stdout)
	}
	if hits.downloads() != 1 {
		t.Fatalf("downloads %d", hits.downloads())
	}
}

func TestUpdateUsesTheModuleVersion(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		prepare(t)
		dest := writeFakeBinary(t, []byte("old"))
		pinUpdate(t, "dev", dest)
		updateBuildVersion = func() string { return "v0.1.0" }
		hits := serveReleases(t, releaseSpec{tag: "v0.1.0", asset: elfBinary()})

		stdout, _, err := startCLI(t, "", "update")
		if err != nil {
			t.Fatal(err)
		}
		if stdout != "already up to date\n" {
			t.Fatalf("stdout %q", stdout)
		}
		if hits.downloads() != 0 {
			t.Fatalf("downloads %d", hits.downloads())
		}
	})

	t.Run("older", func(t *testing.T) {
		prepare(t)
		dest := writeFakeBinary(t, []byte("old"))
		pinUpdate(t, "dev", dest)
		updateBuildVersion = func() string { return "v0.1.0" }
		hits := serveReleases(t, releaseSpec{tag: "v0.2.0", asset: elfBinary()})

		stdout, _, err := startCLI(t, "", "update")
		if err != nil {
			t.Fatal(err)
		}
		resolved := resolvedPath(t, dest)
		if stdout != fmt.Sprintf("updated %s to v0.2.0\n", resolved) {
			t.Fatalf("stdout %q", stdout)
		}
		if hits.downloads() != 1 {
			t.Fatalf("downloads %d", hits.downloads())
		}
	})
}

func TestCurrentReleaseVersionPrefersTheBuild(t *testing.T) {
	prevVersion := updateVersion
	prevBuild := updateBuildVersion
	t.Cleanup(func() {
		updateVersion = prevVersion
		updateBuildVersion = prevBuild
	})
	updateVersion = func() string { return "0.2.0" }
	updateBuildVersion = func() string { return "v0.1.0" }
	if got := currentReleaseVersion(); got != "0.2.0" {
		t.Fatalf("version %s", got)
	}
}

func TestUpdateReportsReleaseErrors(t *testing.T) {
	tests := []struct {
		name string
		spec releaseSpec
		want string
	}{
		{
			name: "no release",
			spec: releaseSpec{latestStatus: http.StatusNotFound},
			want: "no release",
		},
		{
			name: "prerelease",
			spec: releaseSpec{
				latestStatus: http.StatusNotFound,
				list:         `[{"tag_name":"v0.2.0-rc.1"}]`,
			},
			want: "no stable release. The newest prerelease is v0.2.0-rc.1",
		},
		{
			name: "github status",
			spec: releaseSpec{latestStatus: http.StatusInternalServerError},
			want: "GitHub returned 500 for the latest release",
		},
		{
			name: "list status",
			spec: releaseSpec{
				latestStatus: http.StatusNotFound,
				listStatus:   http.StatusBadGateway,
			},
			want: "GitHub returned 502 while listing releases",
		},
		{
			name: "bare tag",
			spec: releaseSpec{tag: "0.2.0"},
			want: "release tag 0.2.0 has no leading v",
		},
		{
			name: "empty tag",
			spec: releaseSpec{tag: ""},
			want: "latest release has no tag",
		},
		{
			name: "invalid version",
			spec: releaseSpec{tag: "v0.2.0/../x"},
			want: "invalid version: v0.2.0/../x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepare(t)
			dest := writeFakeBinary(t, []byte("old"))
			pinUpdate(t, "0.1.0", dest)
			tt.spec.asset = elfBinary()
			hits := serveReleases(t, tt.spec)

			stdout, stderr, err := startCLI(t, "", "update")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err %v", err)
			}
			if stdout != "" {
				t.Fatalf("stdout %q", stdout)
			}
			if strings.Contains(stderr, "Usage:") {
				t.Fatalf("usage leaked %q", stderr)
			}
			if hits.downloads() != 0 {
				t.Fatalf("downloads %d", hits.downloads())
			}
			if tt.spec.latestStatus == http.StatusNotFound && hits.query() != "per_page=1" {
				t.Fatalf("query %s", hits.query())
			}
			if got := mustRead(t, dest); string(got) != "old" {
				t.Fatalf("binary %q", got)
			}
		})
	}
}

func TestUpdateRejectsANonExecutable(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "0.1.0", dest)
	serveReleases(t, releaseSpec{tag: "v0.2.0", asset: []byte("not an executable!!")})

	_, _, err := startCLI(t, "", "update")
	if err == nil || !strings.Contains(err.Error(), "downloaded file is not an executable") {
		t.Fatalf("err %v", err)
	}
	if got := mustRead(t, dest); string(got) != "old" {
		t.Fatalf("binary %q", got)
	}
}

func TestUpdateRejectsAnEmptyDownload(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "0.1.0", dest)
	serveReleases(t, releaseSpec{tag: "v0.2.0", asset: []byte{}})

	_, _, err := startCLI(t, "", "update")
	if err == nil || !strings.Contains(err.Error(), "downloaded an empty file") {
		t.Fatalf("err %v", err)
	}
	if got := mustRead(t, dest); string(got) != "old" {
		t.Fatalf("binary %q", got)
	}
}

func TestUpdateKeepsTheBinaryOnDownloadFailure(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "0.1.0", dest)
	serveReleases(t, releaseSpec{
		tag:         "v0.2.0",
		asset:       elfBinary(),
		assetStatus: http.StatusNotFound,
	})

	_, _, err := startCLI(t, "", "update")
	if err == nil || !strings.Contains(err.Error(), "failed to download") {
		t.Fatalf("err %v", err)
	}
	if got := mustRead(t, dest); string(got) != "old" {
		t.Fatalf("binary %q", got)
	}
}

func TestUpdateInstallsWithPrivilegeWhenTheDirectoryIsLocked(t *testing.T) {
	prepare(t)
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "co")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	pinUpdate(t, "0.1.0", dest)
	asset := elfBinary()
	var installed []byte
	installPrivileged = func(src, got string) error {
		if got != resolvedPath(t, dest) {
			return fmt.Errorf("dest %s", got)
		}
		body, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		installed = body
		if err := os.Chmod(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(got, body, 0o755)
	}
	hits := serveReleases(t, releaseSpec{tag: "v0.2.0", asset: asset})

	stdout, _, err := startCLI(t, "", "update")
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolvedPath(t, dest)
	if stdout != fmt.Sprintf("updated %s to v0.2.0\n", resolved) {
		t.Fatalf("stdout %q", stdout)
	}
	if hits.downloads() != 1 {
		t.Fatalf("downloads %d", hits.downloads())
	}
	if string(installed) != string(asset) {
		t.Fatalf("installed %q", installed)
	}
	if got := mustRead(t, dest); string(got) != string(asset) {
		t.Fatalf("binary %q", got)
	}
}

func TestUpdateRejectsAnExtraArgument(t *testing.T) {
	prepare(t)
	dest := writeFakeBinary(t, []byte("old"))
	pinUpdate(t, "0.1.0", dest)

	stdout, stderr, err := startCLI(t, "", "update", "now")
	if err == nil || !strings.Contains(err.Error(), `unknown command "now"`) {
		t.Fatalf("err %v", err)
	}
	if stdout != "" || strings.Contains(stderr, "Usage:") {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	if got := mustRead(t, dest); string(got) != "old" {
		t.Fatalf("binary %q", got)
	}
}

func TestEphemeralBinary(t *testing.T) {
	ordinary := filepath.Join(t.TempDir(), "co")
	if ephemeralBinary(ordinary) {
		t.Fatalf("%s was ephemeral", ordinary)
	}

	cache := t.TempDir()
	t.Setenv("GOCACHE", cache)
	cached := filepath.Join(cache, "89", "89a191-d", "co")
	if !ephemeralBinary(cached) {
		t.Fatalf("%s was not ephemeral", cached)
	}
	t.Setenv("GOCACHE", "off")
	if ephemeralBinary(cached) {
		t.Fatalf("%s stayed ephemeral with GOCACHE off", cached)
	}

	named := filepath.Join(t.TempDir(), "go-build", "co")
	if !ephemeralBinary(named) {
		t.Fatalf("%s was not ephemeral", named)
	}
}

func TestParseVersionLine(t *testing.T) {
	got, err := parseVersionLine("co 0.1.2 (commit: 9c0c2b2, built: 2026-11-12T11:29:00Z)\ngo: go1.26.4\n")
	if err != nil || got != "0.1.2" {
		t.Fatalf("version %q err %v", got, err)
	}
	if _, err := parseVersionLine("not co"); err == nil {
		t.Fatal("accepted unexpected output")
	}
}

func TestSameRelease(t *testing.T) {
	tests := []struct {
		name    string
		current string
		ver     string
		want    bool
	}{
		{name: "equal", current: "0.1.0", ver: "0.1.0", want: true},
		{name: "leading v", current: "v0.1.0", ver: "0.1.0", want: true},
		{name: "dev", current: "dev", ver: "0.1.0", want: false},
		{name: "devel", current: "(devel)", ver: "0.1.0", want: false},
		{name: "prerelease", current: "0.1.0-rc.1", ver: "0.1.0", want: false},
		{name: "empty current", current: "", ver: "0.1.0", want: false},
		{name: "empty release", current: "0.1.0", ver: "", want: false},
		{name: "newer", current: "0.2.0", ver: "0.1.0", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameRelease(tt.current, tt.ver); got != tt.want {
				t.Fatalf("sameRelease(%q, %q) = %v", tt.current, tt.ver, got)
			}
		})
	}
}

func TestKnownExecutable(t *testing.T) {
	tests := []struct {
		name string
		hdr  [4]byte
		want bool
	}{
		{name: "elf", hdr: [4]byte{0x7f, 'E', 'L', 'F'}, want: true},
		{name: "macho64le", hdr: [4]byte{0xcf, 0xfa, 0xed, 0xfe}, want: true},
		{name: "macho32le", hdr: [4]byte{0xce, 0xfa, 0xed, 0xfe}, want: true},
		{name: "macho32be", hdr: [4]byte{0xfe, 0xed, 0xfa, 0xce}, want: true},
		{name: "macho64be", hdr: [4]byte{0xfe, 0xed, 0xfa, 0xcf}, want: true},
		{name: "text", hdr: [4]byte{'n', 'o', 't', ' '}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := knownExecutable(tt.hdr); got != tt.want {
				t.Fatalf("knownExecutable(%x) = %v", tt.hdr, got)
			}
		})
	}
}

func pinUpdate(t *testing.T, current, dest string) {
	t.Helper()
	prevVersion := updateVersion
	prevBuild := updateBuildVersion
	prevExe := updateExecutable
	prevLook := updateLookPath
	prevInstalled := updateInstalledVersion
	prevInstall := installPrivileged
	t.Cleanup(func() {
		updateVersion = prevVersion
		updateBuildVersion = prevBuild
		updateExecutable = prevExe
		updateLookPath = prevLook
		updateInstalledVersion = prevInstalled
		installPrivileged = prevInstall
	})
	updateVersion = func() string { return current }
	updateBuildVersion = func() string { return "(devel)" }
	updateExecutable = func() (string, error) { return dest, nil }
	updateLookPath = func(name string) (string, error) {
		return "", fmt.Errorf("PATH lookup was not expected for %s", name)
	}
	updateInstalledVersion = func(context.Context, string) (string, error) {
		return "", errors.New("installed version was not expected")
	}
	installPrivileged = func(_, installed string) error {
		return fmt.Errorf("privileged install was not expected for %s", installed)
	}
}

func writeGoRunBinary(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(
		t.TempDir(),
		"go-build",
		"89",
		"89a1915797b4a12d8e86ac6a300af129b9e492b3191f4ee75fe86a30411d3037-d",
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "co")
	if err := os.WriteFile(dest, []byte("runner"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dest
}

func writeFakeBinary(t *testing.T, body []byte) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "co")
	if err := os.WriteFile(dest, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return dest
}

func resolvedPath(t *testing.T, file string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mustRead(t *testing.T, file string) []byte {
	t.Helper()
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func elfBinary() []byte {
	return []byte{0x7f, 'E', 'L', 'F', 'c', 'o'}
}

type releaseSpec struct {
	tag          string
	latestStatus int
	list         string
	listStatus   int
	asset        []byte
	assetStatus  int
}

type releaseHits struct {
	mu        sync.Mutex
	got       int
	asset     string
	listQuery string
}

func (h *releaseHits) downloads() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.got
}

func (h *releaseHits) assetPath() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.asset
}

func (h *releaseHits) query() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listQuery
}

func serveReleases(t *testing.T, spec releaseSpec) *releaseHits {
	t.Helper()
	hits := &releaseHits{}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+releaseRepo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if spec.latestStatus != 0 {
			w.WriteHeader(spec.latestStatus)
			return
		}
		body, err := json.Marshal(map[string]string{"tag_name": spec.tag})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/repos/"+releaseRepo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		hits.mu.Lock()
		hits.listQuery = r.URL.RawQuery
		hits.mu.Unlock()
		if spec.listStatus != 0 {
			w.WriteHeader(spec.listStatus)
			return
		}
		if spec.list == "" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		_, _ = w.Write([]byte(spec.list))
	})
	mux.HandleFunc("/"+releaseRepo+"/releases/download/", func(w http.ResponseWriter, r *http.Request) {
		hits.mu.Lock()
		hits.got++
		hits.asset = r.URL.Path
		hits.mu.Unlock()
		if spec.assetStatus != 0 {
			w.WriteHeader(spec.assetStatus)
			return
		}
		_, _ = w.Write(spec.asset)
	})

	srv := httptest.NewServer(mux)
	prevAPI := githubAPI
	prevDownload := githubDownload
	githubAPI = srv.URL
	githubDownload = srv.URL
	t.Cleanup(func() {
		srv.Close()
		githubAPI = prevAPI
		githubDownload = prevDownload
	})
	return hits
}
