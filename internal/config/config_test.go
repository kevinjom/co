package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func reset(t *testing.T) {
	t.Helper()
	viper.Reset()
	current = Settings{}
	t.Cleanup(func() {
		viper.Reset()
		current = Settings{}
	})
}

func TestEnsureFile(t *testing.T) {
	reset(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".co", "config.yaml")
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", info.Mode().Perm())
	}

	viper.SetConfigFile(path)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != slog.LevelInfo || got.LogRetentionDays != DefaultLogRetentionDays || got.MaxToolRounds != DefaultMaxToolRounds || got.Theme != DefaultTheme {
		t.Fatalf("%+v", got)
	}
	if got.NoHistory || got.ClearScrollback || got.Resume != "" {
		t.Fatalf("%+v", got)
	}
	if viper.GetString("model") != "" {
		t.Fatalf("model %q", viper.GetString("model"))
	}

	kept := "log-level: debug\n"
	if err := os.WriteFile(path, []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != kept {
		t.Fatalf("config changed:\n%s", data)
	}
}

func TestLoadUsesDefaults(t *testing.T) {
	reset(t)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != slog.LevelInfo || got.LogRetentionDays != DefaultLogRetentionDays || got.MaxToolRounds != DefaultMaxToolRounds || got.Limit != DefaultSessionLimit || got.Theme != DefaultTheme {
		t.Fatalf("%+v", got)
	}
	if got.NoHistory || got.ClearScrollback || got.Resume != "" {
		t.Fatalf("%+v", got)
	}
}

func TestFileThenEnv(t *testing.T) {
	reset(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "log-level: debug\nno-history: true\nmax-tool-rounds: 8\nlimit: 5\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != slog.LevelDebug || !got.NoHistory || got.MaxToolRounds != 8 || got.Limit != 5 {
		t.Fatalf("%+v", got)
	}

	t.Setenv("CO_LOG_LEVEL", "error")
	t.Setenv("CO_NO_HISTORY", "false")
	t.Setenv("CO_MAX_TOOL_ROUNDS", "3")
	got, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Level != slog.LevelError || got.NoHistory || got.MaxToolRounds != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	reset(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("max-tool-rounds: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "max-tool-rounds") {
		t.Fatalf("err = %v", err)
	}

	viper.Set("max-tool-rounds", DefaultMaxToolRounds)
	viper.Set("log-level", "loud")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "log-level") {
		t.Fatalf("err = %v", err)
	}
}

func TestLogRetentionDays(t *testing.T) {
	reset(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("log-retention-days: 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	viper.SetConfigFile(path)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.LogRetentionDays != 5 {
		t.Fatalf("retention %d", got.LogRetentionDays)
	}

	t.Setenv("CO_LOG_RETENTION_DAYS", "2")
	got, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.LogRetentionDays != 2 {
		t.Fatalf("retention %d", got.LogRetentionDays)
	}

	viper.Set("log-retention-days", 0)
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "log-retention-days") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsTheme(t *testing.T) {
	reset(t)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	viper.Set("theme", "nope")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "theme") {
		t.Fatalf("err = %v", err)
	}
}

func TestExplicitModelIgnoresFile(t *testing.T) {
	reset(t)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	viper.Set("model", "file/model")
	if ExplicitModel(false) != "" {
		t.Fatal("file model was treated as explicit")
	}
	t.Setenv("CO_MODEL", "env/model")
	if ExplicitModel(false) != "env/model" {
		t.Fatalf("env model %q", ExplicitModel(false))
	}
	viper.Set("model", "flag/model")
	if ExplicitModel(true) != "flag/model" {
		t.Fatalf("flag model %q", ExplicitModel(true))
	}
}

func TestClearResumeKeepsFlagAndEnv(t *testing.T) {
	reset(t)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	viper.Set("resume", "from-file")
	ClearResume(false)
	if viper.GetString("resume") != "" {
		t.Fatalf("resume %q", viper.GetString("resume"))
	}

	viper.Set("resume", "from-flag")
	ClearResume(true)
	if viper.GetString("resume") != "from-flag" {
		t.Fatalf("resume %q", viper.GetString("resume"))
	}

	t.Setenv("CO_RESUME", "from-env")
	viper.Set("resume", "from-env")
	ClearResume(false)
	if viper.GetString("resume") != "from-env" {
		t.Fatalf("resume %q", viper.GetString("resume"))
	}
}

func TestSessionLimit(t *testing.T) {
	reset(t)
	if err := Prepare(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	got, err := SessionLimit(false)
	if err != nil || got != DefaultSessionLimit {
		t.Fatalf("limit %d err %v", got, err)
	}

	t.Setenv("CO_LIMIT", "many")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	_, err = SessionLimit(false)
	if err == nil || !strings.Contains(err.Error(), "invalid limit") {
		t.Fatalf("err = %v", err)
	}

	t.Setenv("CO_LIMIT", "1")
	viper.Set("limit", 4)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	got, err = SessionLimit(true)
	if err != nil || got != 4 {
		t.Fatalf("limit %d err %v", got, err)
	}
}
