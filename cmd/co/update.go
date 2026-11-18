package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	releaseRepo    = "kevinjom/co"
	updateTimeout  = 2 * time.Minute
	maxReleaseBody = 1 << 20
	maxReleaseFile = 128 << 20
)

// updateVersion, updateBuildVersion, updateExecutable, updateLookPath,
// updateInstalledVersion, and installPrivileged are the seams tests replace.
// A directory that is not writable is installed with sudo, the same way
// install.sh installs into a protected prefix.
var (
	updateVersion          = func() string { return version }
	updateBuildVersion     = readBuildVersion
	updateExecutable       = os.Executable
	updateLookPath         = exec.LookPath
	updateInstalledVersion = readInstalledVersion
	installPrivileged      = sudoInstall
	githubAPI              = "https://api.github.com"
	githubDownload         = "https://github.com"
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "update",
		Short:         "Update co to the latest release",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runUpdate,
	}
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	signals := startSignals(parent)
	defer signals.close()

	ctx, cancel := context.WithTimeout(signals.ctx, updateTimeout)
	defer cancel()
	err := applyUpdate(ctx, cmd.OutOrStdout())
	if sigErr := signals.err(); err != nil && sigErr != nil {
		return sigErr
	}
	return err
}

func applyUpdate(ctx context.Context, w io.Writer) error {
	dest, have, err := binaryToUpdate(ctx)
	if err != nil {
		return err
	}
	tag, ver, err := latestStable(ctx)
	if err != nil {
		return err
	}
	if sameRelease(have, ver) {
		_, err := fmt.Fprintln(w, "already up to date")
		return err
	}

	goos, goarch, err := releasePlatform()
	if err != nil {
		return err
	}
	asset, err := releaseAssetURL(tag, ver, goos, goarch)
	if err != nil {
		return err
	}
	if err := installRelease(ctx, asset, dest); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "updated %s to %s\n", dest, tag)
	return err
}

// currentReleaseVersion is the version embedded at build time.
// A local build leaves that as dev, and then the module version recorded
// by go install is used.
func currentReleaseVersion() string {
	current := strings.TrimSpace(updateVersion())
	if current != "" && current != "dev" {
		return current
	}
	module := strings.TrimSpace(updateBuildVersion())
	if module == "" || module == "(devel)" {
		return current
	}
	return module
}

func readBuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}

func sameRelease(current, ver string) bool {
	current = strings.TrimSpace(current)
	current, _ = strings.CutPrefix(current, "v")
	return ver != "" && current == ver
}

func latestStable(ctx context.Context) (string, string, error) {
	rawURL, err := githubURL(
		githubAPI,
		"",
		"repos",
		releaseRepo,
		"releases",
		"latest",
	)
	if err != nil {
		return "", "", err
	}
	body, status, err := readGitHub(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	if status == http.StatusNotFound {
		return noStableRelease(ctx)
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("GitHub returned %d for the latest release", status)
	}
	tag, err := releaseTag(body)
	if err != nil {
		return "", "", err
	}
	ver, err := versionFromTag(tag)
	if err != nil {
		return "", "", err
	}
	return tag, ver, nil
}

// noStableRelease distinguishes a missing release from a newest prerelease.
// /releases/latest is 404 when every published release is a prerelease.
func noStableRelease(ctx context.Context) (string, string, error) {
	rawURL, err := githubURL(
		githubAPI,
		"per_page=1",
		"repos",
		releaseRepo,
		"releases",
	)
	if err != nil {
		return "", "", err
	}
	body, status, err := readGitHub(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("GitHub returned %d while listing releases", status)
	}
	tag, err := newestTag(body)
	if err != nil {
		return "", "", err
	}
	if tag != "" {
		return "", "", fmt.Errorf("no stable release. The newest prerelease is %s", tag)
	}
	return "", "", errors.New("no release")
}

func releaseTag(body []byte) (string, error) {
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("reading the latest release: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("latest release has no tag")
	}
	return release.TagName, nil
}

func newestTag(body []byte) (string, error) {
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("reading releases: %w", err)
	}
	if len(releases) == 0 {
		return "", nil
	}
	return releases[0].TagName, nil
}

func versionFromTag(tag string) (string, error) {
	ver, ok := strings.CutPrefix(tag, "v")
	if !ok {
		return "", fmt.Errorf("release tag %s has no leading v", tag)
	}
	if ver == "" || strings.ContainsFunc(ver, invalidVersionRune) {
		return "", fmt.Errorf("invalid version: %s", tag)
	}
	return ver, nil
}

func invalidVersionRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
	case r >= 'A' && r <= 'Z':
	case r >= 'a' && r <= 'z':
	case r == '.', r == '_', r == '-':
	default:
		return true
	}
	return false
}

func releasePlatform() (string, string, error) {
	var goos string
	switch runtime.GOOS {
	case "darwin", "linux":
		goos = runtime.GOOS
	default:
		return "", "", fmt.Errorf("unsupported system: %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return goos, runtime.GOARCH, nil
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
}

func releaseAssetURL(tag, ver, goos, goarch string) (string, error) {
	name := fmt.Sprintf("co_%s_%s_%s", ver, goos, goarch)
	return githubURL(
		githubDownload,
		"",
		releaseRepo,
		"releases",
		"download",
		tag,
		name,
	)
}

func githubURL(base, query string, elem ...string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("failed to reach GitHub: %w", err)
	}
	parts := make([]string, 0, len(elem)+1)
	if u.Path != "" && u.Path != "/" {
		parts = append(parts, strings.TrimPrefix(u.Path, "/"))
	}
	parts = append(parts, elem...)
	u.Path = "/" + path.Join(parts...)
	u.RawQuery = query
	return u.String(), nil
}

// binaryToUpdate is the file co update replaces, and the version that file
// reports. go run stores its process in the build cache, so that copy is
// left alone and the co on PATH is updated instead.
func binaryToUpdate(ctx context.Context) (string, string, error) {
	current, err := resolveExecutable()
	if err != nil {
		return "", "", err
	}
	if !ephemeralBinary(current) {
		return current, currentReleaseVersion(), nil
	}
	dest, err := installedOnPath()
	if err != nil {
		return "", "", err
	}
	if dest == current || ephemeralBinary(dest) {
		return "", "", errors.New("no installed co on PATH")
	}
	ver, err := updateInstalledVersion(ctx, dest)
	if err != nil {
		return "", "", err
	}
	return dest, ver, nil
}

func resolveExecutable() (string, error) {
	exe, err := updateExecutable()
	if err != nil {
		return "", fmt.Errorf("finding the co binary: %w", err)
	}
	return resolveFile(exe)
}

func installedOnPath() (string, error) {
	found, err := updateLookPath("co")
	if err != nil {
		return "", errors.New("no installed co on PATH")
	}
	return resolveFile(found)
}

func resolveFile(file string) (string, error) {
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", fmt.Errorf("finding the co binary: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("finding the co binary: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", resolved)
	}
	return resolved, nil
}

// ephemeralBinary reports a go run executable. Those files live in the
// build cache and are discarded after the process exits.
func ephemeralBinary(file string) bool {
	cleaned := filepath.Clean(file)
	if cache := goBuildCache(); cache != "" && underDir(cleaned, cache) {
		return true
	}
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == "go-build" {
			return true
		}
	}
	return false
}

func goBuildCache() string {
	env := strings.TrimSpace(os.Getenv("GOCACHE"))
	if env == "off" {
		return ""
	}
	if env != "" {
		return filepath.Clean(env)
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "go-build")
}

func underDir(file, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), file)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func readInstalledVersion(ctx context.Context, file string) (string, error) {
	cmd := exec.CommandContext(ctx, file, "version")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("reading version of %s: %w", file, err)
	}
	return parseVersionLine(string(out))
}

func parseVersionLine(out string) (string, error) {
	line, _, _ := strings.Cut(out, "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "co" {
		return "", fmt.Errorf("reading version: unexpected output %q", strings.TrimSpace(line))
	}
	return fields[1], nil
}

func installRelease(ctx context.Context, asset, dest string) error {
	tmp, direct, err := updateTemp(dest)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := writeDownload(ctx, asset, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("installing %s: %w", dest, err)
	}
	if err := checkExecutable(tmpPath, asset); err != nil {
		return err
	}
	if !direct {
		return installPrivileged(tmpPath, dest)
	}
	return swapIntoPlace(tmpPath, dest)
}

// updateTemp creates the new binary beside dest so the later rename is atomic.
// A directory the user cannot write falls back to the system temp directory,
// and the file is installed with sudo.
func updateTemp(dest string) (*os.File, bool, error) {
	f, err := os.CreateTemp(filepath.Dir(dest), ".co-update-*")
	if err == nil {
		return f, true, nil
	}
	if !os.IsPermission(err) {
		return nil, false, fmt.Errorf("installing %s: %w", dest, err)
	}
	f, err = os.CreateTemp("", "co-update-*")
	if err != nil {
		return nil, false, fmt.Errorf("installing %s: %w", dest, err)
	}
	return f, false, nil
}

func swapIntoPlace(src, dest string) error {
	if err := os.Chmod(src, installMode(dest)); err != nil {
		return fmt.Errorf("installing %s: %w", dest, err)
	}
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("installing %s: %w", dest, err)
	}
	return nil
}

func installMode(dest string) os.FileMode {
	info, err := os.Stat(dest)
	if err != nil || info.IsDir() {
		return 0o755
	}
	perm := info.Mode().Perm()
	if perm&0o111 == 0 {
		return 0o755
	}
	return perm
}

func sudoInstall(src, dest string) error {
	if _, err := exec.LookPath("sudo"); err != nil {
		return fmt.Errorf("%s is not writable", dest)
	}
	cmd := exec.Command("sudo", "install", "-m", "755", "--", src, dest)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing %s: %w", dest, err)
	}
	return nil
}

func writeDownload(ctx context.Context, asset string, dst *os.File) error {
	resp, err := httpGET(
		ctx,
		asset,
		"",
		"failed to download "+asset,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s", asset)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, maxReleaseFile+1))
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", asset, err)
	}
	if n == 0 {
		return fmt.Errorf("downloaded an empty file from %s", asset)
	}
	if n > maxReleaseFile {
		return fmt.Errorf("downloaded file from %s is too large", asset)
	}
	return nil
}

func checkExecutable(file, asset string) error {
	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("downloaded file is not an executable")
	}
	if info.Size() == 0 {
		return fmt.Errorf("downloaded an empty file from %s", asset)
	}
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("downloaded file is not an executable")
	}
	defer f.Close()
	var hdr [4]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return fmt.Errorf("downloaded file is not an executable")
	}
	if !knownExecutable(hdr) {
		return fmt.Errorf("downloaded file is not an executable")
	}
	return nil
}

func knownExecutable(hdr [4]byte) bool {
	switch hdr {
	case [4]byte{0x7f, 'E', 'L', 'F'}:
		return true
	case [4]byte{0xcf, 0xfa, 0xed, 0xfe},
		[4]byte{0xce, 0xfa, 0xed, 0xfe},
		[4]byte{0xfe, 0xed, 0xfa, 0xce},
		[4]byte{0xfe, 0xed, 0xfa, 0xcf}:
		return true
	default:
		return false
	}
}

func readGitHub(ctx context.Context, rawURL string) ([]byte, int, error) {
	resp, err := httpGET(
		ctx,
		rawURL,
		"application/vnd.github+json",
		"failed to reach GitHub",
	)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseBody))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to reach GitHub: %w", err)
	}
	return body, resp.StatusCode, nil
}

func httpGET(ctx context.Context, rawURL, accept, failure string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", failure, err)
	}
	req.Header.Set("User-Agent", "co/"+strings.TrimSpace(updateVersion()))
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", failure, err)
	}
	return resp, nil
}
