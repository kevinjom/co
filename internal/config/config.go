// Package config loads process settings through viper.
// Precedence is the flag, then the environment, then config.yaml, then the defaults here.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/kevinjom/co/internal/theme"

	"github.com/spf13/viper"
)

const (
	// DefaultLogLevel is the log level when log-level is unset.
	DefaultLogLevel = "info"
	// DefaultMaxToolRounds is how many model calls may include tools when max-tool-rounds is unset.
	// One more call is then made with no tools.
	DefaultMaxToolRounds = 64
	// DefaultSessionLimit is how many sessions `co session list` prints when limit is unset.
	DefaultSessionLimit = 20
	// DefaultTheme is the TUI palette when theme is unset.
	DefaultTheme = theme.Default
)

// Settings is the resolved process configuration.
type Settings struct {
	Level           slog.Level
	NoHistory       bool
	ClearScrollback bool
	MaxToolRounds   int
	Resume          string
	Limit           int
	Theme           string
}

type setting struct {
	key        string
	env        string
	def        any
	hasDefault bool
}

// settings is every viper key. model and resume have no default:
// model is chosen later, and resume must not stick across launches.
var settings = []setting{
	{key: "model", env: "CO_MODEL"},
	{key: "log-level", env: "CO_LOG_LEVEL", def: DefaultLogLevel, hasDefault: true},
	{key: "resume", env: "CO_RESUME"},
	{key: "no-history", env: "CO_NO_HISTORY", def: false, hasDefault: true},
	{key: "clear-scrollback", env: "CO_CLEAR_SCROLLBACK", def: false, hasDefault: true},
	{key: "max-tool-rounds", env: "CO_MAX_TOOL_ROUNDS", def: DefaultMaxToolRounds, hasDefault: true},
	{key: "limit", env: "CO_LIMIT", def: DefaultSessionLimit, hasDefault: true},
	{key: "theme", env: "CO_THEME", def: DefaultTheme, hasDefault: true},
}

var current Settings

// Prepare binds environment variables and installs defaults.
// The caller reads the config file after this returns.
func Prepare() error {
	viper.SetEnvPrefix("CO")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	for _, s := range settings {
		if err := viper.BindEnv(s.key, s.env); err != nil {
			return fmt.Errorf("binding %s: %w", s.key, err)
		}
		if s.hasDefault {
			viper.SetDefault(s.key, s.def)
		}
	}
	return nil
}

// Load reads the resolved settings. resume in the file is kept only when
// ClearResume has not already removed it.
func Load() (Settings, error) {
	level, err := parseLogLevel(viper.GetString("log-level"))
	if err != nil {
		return Settings{}, err
	}
	rounds := viper.GetInt("max-tool-rounds")
	if rounds < 1 {
		return Settings{}, fmt.Errorf("invalid max-tool-rounds %v", viper.Get("max-tool-rounds"))
	}
	name, err := theme.Parse(viper.GetString("theme"))
	if err != nil {
		return Settings{}, err
	}
	current = Settings{
		Level:           level,
		NoHistory:       viper.GetBool("no-history"),
		ClearScrollback: viper.GetBool("clear-scrollback"),
		MaxToolRounds:   rounds,
		Resume:          strings.TrimSpace(viper.GetString("resume")),
		Limit:           viper.GetInt("limit"),
		Theme:           name,
	}
	return current, nil
}

// Current is the last successful Load.
func Current() Settings {
	return current
}

// ExplicitModel is the model from the flag or CO_MODEL.
// A model key in the config file is left for the provider catalog.
func ExplicitModel(flagChanged bool) string {
	if flagChanged {
		return strings.TrimSpace(viper.GetString("model"))
	}
	return strings.TrimSpace(os.Getenv("CO_MODEL"))
}

// ClearResume drops a resume value that came from the config file.
// The flag and CO_RESUME still apply to this process.
func ClearResume(flagChanged bool) {
	if strings.TrimSpace(os.Getenv("CO_RESUME")) != "" {
		return
	}
	if flagChanged {
		return
	}
	viper.Set("resume", "")
}

// SessionLimit is the list size for this process.
// flagChanged reports that --limit was set on the command line.
func SessionLimit(flagChanged bool) (int, error) {
	if !flagChanged {
		if env, ok := os.LookupEnv("CO_LIMIT"); ok && strings.TrimSpace(env) != "" {
			raw := strings.TrimSpace(env)
			if _, err := strconv.Atoi(raw); err != nil {
				return 0, fmt.Errorf("invalid limit %q", raw)
			}
		}
	}
	limit := Current().Limit
	if limit < 1 {
		return 0, fmt.Errorf("limit must be at least 1")
	}
	return limit, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log-level %q", raw)
	}
}
