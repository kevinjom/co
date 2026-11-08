package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/provider"

	"github.com/spf13/cobra"
)

const (
	defaultOllamaHost  = "http://127.0.0.1:11434"
	ollamaProbeTimeout = 500 * time.Millisecond
	ollamaStartWait    = 10 * time.Second
)

// ollamaLookPath and ollamaStart are the process seams. Tests replace them.
var (
	ollamaLookPath = exec.LookPath
	ollamaStart    = startOllamaServe
)

// hintUnknownProvider tells the user how to add Ollama when that provider is missing.
func hintUnknownProvider(err error) error {
	var unknown *provider.UnknownProviderError
	if errors.As(err, &unknown) && unknown.Provider == "ollama" {
		return fmt.Errorf("%w: run co ollama", err)
	}
	return err
}

func newOllamaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ollama",
		Short:         "Choose an Ollama model and open the session",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runOllama,
	}
	cmd.Flags().String("host", defaultOllamaHost, "Ollama host")
	return cmd
}

func runOllama(cmd *cobra.Command, _ []string) error {
	if _, err := ollamaLookPath("ollama"); err != nil {
		return fmt.Errorf("ollama is not installed")
	}
	host, err := ollamaHost(cmd)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	running, err := runningModels(ctx, host)
	started := false
	if daemonUnreachable(err) {
		if !localDaemon(host) {
			return fmt.Errorf("ollama at %s is not running", host)
		}
		if err := ollamaStart(host); err != nil {
			return err
		}
		started = true
		running, err = waitForDaemon(ctx, host)
	}
	if err != nil {
		return err
	}

	installed, err := listDaemonModels(ctx, host)
	if err != nil {
		return err
	}
	chosen, err := chooseOllamaModel(cmd, started, running, installed)
	if err != nil {
		return err
	}
	if !slices.Contains(installed, chosen) {
		installed = append(installed, chosen)
		slices.Sort(installed)
	}

	configPath, modelsPath, err := ollamaFiles(cmd)
	if err != nil {
		return err
	}
	spec := provider.ProviderSpec{
		ID:       "ollama",
		BaseURL:  host,
		AuthNone: true,
		Models:   installed,
		Model:    "ollama/" + chosen,
		CompatOnCreate: &llm.Compat{
			ReasoningEffort: true,
			ReasoningDelta:  true,
		},
	}
	if err := provider.WriteProvider(configPath, modelsPath, spec); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "model: ollama/%s\n", chosen)
	if isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout()) {
		return start(cmd, "ollama/"+chosen)
	}
	return nil
}

// chooseOllamaModel uses the loaded model when the daemon was already running.
// Starting the daemon, or finding none loaded, asks which installed model to use.
func chooseOllamaModel(cmd *cobra.Command, started bool, running, installed []string) (string, error) {
	if !started && len(running) == 1 {
		return running[0], nil
	}
	if !started && len(running) > 1 {
		return askModel(cmd, running)
	}
	if len(installed) == 0 {
		return "", fmt.Errorf("ollama has no models")
	}
	return askModel(cmd, installed)
}

func askModel(cmd *cobra.Command, models []string) (string, error) {
	stderr := cmd.ErrOrStderr()
	for i, model := range models {
		fmt.Fprintf(stderr, "%d. %s\n", i+1, model)
	}
	fmt.Fprint(stderr, "Which model? ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading model choice: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", fmt.Errorf("ollama: choose a model")
	}
	if n, err := strconv.Atoi(line); err == nil {
		if n < 1 || n > len(models) {
			return "", fmt.Errorf("ollama: model %q is not in the list", line)
		}
		return models[n-1], nil
	}
	for _, model := range models {
		if model == line {
			return model, nil
		}
	}
	return "", fmt.Errorf("ollama: model %q is not in the list", line)
}

func ollamaHost(cmd *cobra.Command) (string, error) {
	raw := defaultOllamaHost
	if cmd.Flags().Changed("host") {
		value, err := cmd.Flags().GetString("host")
		if err != nil {
			return "", err
		}
		raw = value
	} else if env := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); env != "" {
		raw = env
	}
	host := normalizeHost(raw)
	if host == "" {
		return "", usageError(fmt.Errorf("host is required: set --host or OLLAMA_HOST"))
	}
	return host, nil
}

func normalizeHost(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return "http://" + raw
	}
	return raw
}

func localDaemon(host string) bool {
	parsed, err := url.Parse(host)
	if err != nil {
		return false
	}
	switch parsed.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

// ollamaFiles is the settings file and the provider catalog.
// --config keeps both in that one file. Otherwise the model goes to
// ~/.co/config.yaml and the provider goes to ~/.co/models.yaml.
func ollamaFiles(cmd *cobra.Command) (string, string, error) {
	file, err := cmd.Flags().GetString("config")
	if err != nil {
		return "", "", err
	}
	if file != "" {
		return file, file, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("finding home directory: %w", err)
	}
	return provider.HomeConfig(home), provider.HomeModels(home), nil
}

func startOllamaServe(host string) error {
	cmd := exec.Command("ollama", "serve")
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+ollamaBind(host))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ollama: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func ollamaBind(host string) string {
	parsed, err := url.Parse(host)
	if err != nil || parsed.Host == "" {
		return host
	}
	return parsed.Host
}

func waitForDaemon(ctx context.Context, host string) ([]string, error) {
	deadline := time.NewTimer(ollamaStartWait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		running, err := runningModels(ctx, host)
		if err == nil {
			return running, nil
		}
		if !daemonUnreachable(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("ollama at %s did not start", host)
		case <-ticker.C:
		}
	}
}

func daemonUnreachable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op)
}

func runningModels(ctx context.Context, host string) ([]string, error) {
	body, err := getDaemon(ctx, host, "/api/ps")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		return nil, fmt.Errorf("ollama at %s: %w", host, err)
	}
	ids := make([]string, 0, len(listed.Models))
	seen := map[string]bool{}
	for _, item := range listed.Models {
		name := item.Name
		if name == "" {
			name = item.Model
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		ids = append(ids, name)
	}
	return ids, nil
}

func listDaemonModels(ctx context.Context, host string) ([]string, error) {
	body, err := getDaemon(ctx, host, "/v1/models")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		return nil, fmt.Errorf("ollama at %s: %w", host, err)
	}
	ids := make([]string, 0, len(listed.Data))
	seen := map[string]bool{}
	for _, item := range listed.Data {
		if item.ID == "" || seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	return ids, nil
}

func getDaemon(ctx context.Context, host, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, ollamaProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("ollama at %s: %w", host, err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ollama at %s: %w", host, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama at %s: status %d: %s", host, resp.StatusCode, bytes.TrimSpace(body))
	}
	return body, nil
}
