# co

`co` is a read-only coding assistant for the directory you start it in. It talks to OpenAI-compatible chat endpoints, and the model may list, read, and search files inside that directory. It cannot write files or leave the directory, including through symlinks.

A terminal on both stdin and stdout gets a Bubble Tea TUI, which can hold more than one session. A pipe or a redirect gets a line reader with one session.

## Install

Go 1.26 or newer.

```sh
go install github.com/kevinjom/co/cmd/co@latest
```

To build from a checkout:

```sh
git clone https://github.com/kevinjom/co.git
cd co
go build -o co ./cmd/co
```

`co version` prints the version, commit, and build date. Those stay `dev`, `unknown`, and `unknown` unless you set them:

```sh
go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o co ./cmd/co
```

## Ollama

With the `ollama` binary installed:

```sh
co ollama
```

This talks to `http://127.0.0.1:11434`, starts a local daemon when it is down, and asks you to pick a model when none is already loaded. It writes an `ollama` provider into `~/.co/models.yaml`, the chosen model into `~/.co/config.yaml`, and, in a terminal, opens the session on that model.

```sh
co ollama --host http://127.0.0.1:11434
```

`--host` wins. Otherwise `co ollama` uses `OLLAMA_HOST`, then `http://127.0.0.1:11434`. A host without a scheme is treated as `http://`. `co` only starts the daemon for `127.0.0.1`, `localhost`, and `::1`.

## Configure a provider

Providers live in YAML. A model is `provider/model-id`. The model id may contain further slashes, as in `openrouter/qwen/qwen3-coder`.

`~/.co/config.yaml` is the settings file. It holds the default model and the flags that are safe to keep:

```yaml
model: openrouter/qwen/qwen3-coder
log-level: info
no-history: false
clear-scrollback: false
max-tool-rounds: 64
```

`log-level` is `debug`, `info`, `warn`, or `error`. `no-history` skips the transcript. `clear-scrollback` erases the terminal scrollback before the TUI starts. `max-tool-rounds` is how many model calls in a turn may include tools. After that many, one more call is made with no tools. The default is 64. A flag or its environment variable wins over the file. `resume` is not read from the file.

`~/.co/models.yaml` is the provider catalog:

```yaml
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - qwen3:8b
    compat:
      reasoning_effort: true
      reasoning_delta: true
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
```

`base_url` is required. If it already ends in `/v1` or `/openai`, `co` calls `/chat/completions` on it. Otherwise it calls `/v1/chat/completions`, which is what an Ollama host needs.

`auth: none` means the provider takes no API key. Any other provider needs one. `api_key_env` names an environment variable that is checked first.

`compat.reasoning_effort` sends `reasoning_effort: low` on each request. `compat.reasoning_delta` reads a `reasoning` field from stream chunks and shows it while the model thinks. `co ollama` turns both on when it creates the provider.

### Which file is used

| Source | Role |
| --- | --- |
| `--model`, or `CO_MODEL` | Model for this process. Does not change the config file. |
| `model` in `~/.co/config.yaml` | Default model. |
| `model` in `./.co.yaml` | Default model when the home file has none. |
| `providers` in `~/.co/models.yaml` | Home providers. |
| `providers` in `./.co.yaml` | Replaces the home provider with the same id. Other home providers stay. |
| `log-level`, `no-history`, `clear-scrollback`, `max-tool-rounds` in `~/.co/config.yaml` | Defaults for those flags. |

`--config path` uses that file alone for flags, providers, and the default model. It does not read `~/.co/config.yaml`, `~/.co/models.yaml`, or `./.co.yaml`, and `/models` does not write the home file. Keys still come from the environment and `~/.co/credentials.json`.

`./.co.yaml` is per project. Leave it out of git if it names private endpoints.

## API keys

A key is never stored in the YAML. `co` checks `api_key_env`, then `~/.co/credentials.json`. The credentials file is mode `0600` and its directory is mode `0700`.

In the session, `/connect` lists providers that accept a key. `/connect openrouter` prompts for the key and stores it. The prompt hides what you type. Outside a terminal, set the environment variable instead:

```sh
OPENROUTER_API_KEY=... co --model openrouter/qwen/qwen3-coder
```

## Run

```sh
co
co --model ollama/qwen3:8b
co --resume latest
co --resume 20261102T120000Z-ab12 --no-history
co session list
```

With no model selected, a terminal opens the model picker. Anything else exits and asks for `--model` or `model` in `~/.co/config.yaml`.

| Flag | Environment | `config.yaml` | |
| --- | --- | --- | --- |
| `--config` | | | Replaces the home and project files. |
| `--log-level` | `CO_LOG_LEVEL` | `log-level` | `debug`, `info`, `warn`, or `error`. Default `info`. Logs go to stderr. |
| `--model` | `CO_MODEL` | `model` | `provider/model-id` for this process. The file value is the default model. |
| `--resume` | `CO_RESUME` | | A session id, or `latest` for the newest session in this directory. Not read from the file. |
| `--no-history` | `CO_NO_HISTORY` | `no-history` | Do not write the transcript. |
| `--clear-scrollback` | `CO_CLEAR_SCROLLBACK` | `clear-scrollback` | Erase the terminal scrollback before the TUI starts. Off unless set. |
| `--max-tool-rounds` | `CO_MAX_TOOL_ROUNDS` | `max-tool-rounds` | Model calls that may include tools. Default 64. The next call has no tools. |

`CO_` is the environment prefix, and `-` in a flag name becomes `_`.

### In the session

| Line | |
| --- | --- |
| `/models` | List configured models. In the TUI this opens a picker. |
| `/models provider/model-id` | Switch the model for the session on screen. The choice is saved to `~/.co/config.yaml`, unless `--config` was set. |
| `/connect` | List providers that accept an API key. |
| `/connect provider` | Save an API key for that provider. |
| `/new` | Start another session in the TUI and show it. Other sessions keep running. |
| `/sessions` | Switch the session on screen. In the TUI this opens a picker. |
| `exit`, `quit` | Leave. When a transcript was written, `co` prints the id to resume. |

`/new` and `/sessions` are TUI commands. The line reader keeps a single session.

In the TUI, Tab completes `/models`, `/connect`, `/new`, and `/sessions`. Enter sends, Ctrl+J inserts a newline, and Ctrl+C quits. The transcript fills the terminal, with a session list on the right when the terminal is at least 60 columns wide. Each row is a status icon and the session title. The icon animates while that session is working. The list is only the sessions opened in this TUI. The mouse wheel, trackpad, PgUp, and PgDn scroll the transcript, and it stays pinned to the bottom until you scroll up. Ctrl+C during a turn stops that turn. Other sessions keep going. Ctrl+C while the session on screen is idle quits.

When stdin or stdout is not a terminal, the prompt is `> ` on stderr. The answer is written to stdout. Reasoning and tool lines go to stderr.

## Sessions

Each directory gets its own transcripts:

```
~/.co/projects/<project>/sessions/<id>.jsonl
```

`<project>` is the absolute path with every separator replaced by `-`. The file is mode `0600` and is created on the first completed message, so quitting before that leaves nothing behind. Each session opened in the TUI gets its own file. `--resume latest` opens the most recently modified file for this directory as the first session. A trailing partial line, from a crash during a write, is dropped. The on-disk list below is not the TUI panel.

```sh
co session list
co session list --limit 5
```

`co session list` prints the newest sessions for this directory, 20 by default. `--limit`, `-n`, and `CO_LIMIT` set how many. The first row is the session `--resume latest` would open. Each row is the id, the last update time in UTC, the latest model, and the title. The transcript stores a title with the first prompt: the first line, at most ten words. As soon as that prompt is sent, a background request asks the model for a short name.

## What the model can do

The model gets filesystem tools, jailed to the working directory, and language-server tools when a server binary is on `PATH`. Today that is `gopls` for `.go` files. See [docs/lsp.md](docs/lsp.md).

| Tool | |
| --- | --- |
| `list_dir` | One directory level. Path defaults to `.`. |
| `read_file` | A window of a text file. Defaults to 200 lines from line 1. |
| `grep` | A Go regular expression. Skips `.git`. Returns at most 50 matches. |
| `lsp_definition` | Definition of the symbol at a line and column. |
| `lsp_references` | References to that symbol, including the declaration. |
| `lsp_hover` | Hover text from the language server. |
| `lsp_symbols` | Symbols in one file (`path`) or a workspace search (`query`). |
| `lsp_diagnostics` | Diagnostics for one file, or the workspace when `path` is omitted. |

Paths are resolved, and a path that leaves the directory is rejected. Line and column start at 1. A language-server result may name a file outside the working directory, and it does not include that file's body. Edits, rename, formatting, and code actions are not tools.

## Test

```sh
go test ./...
golangci-lint run
```

## License

[MIT](LICENSE)
