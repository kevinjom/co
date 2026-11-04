# 02 — Providers, sessions, and a fullscreen transcript

Baseline commit: `b745a2560c5627cab47f4723e4b9ed033b82fdd6` (`b745a25`)

| | |
| --- | --- |
| Subject | generate session title |
| Author | kevinjom |
| Date | 2026-11-02 23:14:19 +0800 |
| Branch | `main` |

This file describes what changed after [01](01-readonly-streaming-tui.md). The tools, the path jail, the eight tool rounds, the event set, and the signal exit codes are unchanged. The next iteration starts here:

```
git log --oneline b745a2560c5627cab47f4723e4b9ed033b82fdd6..HEAD
git diff b745a2560c5627cab47f4723e4b9ed033b82fdd6..HEAD
```

## What co is now

`co` still talks to one chat endpoint per turn, and the model may still only list, read, and search inside the working directory. The endpoint is no longer a single Ollama host passed with `--host`. A model is `provider/model-id`. The id after the first slash is sent to that provider and may contain further slashes, as in `openrouter/qwen/qwen3-coder`.

The module path is `github.com/kevinjom/co`, Go 1.26. Direct dependencies add `charm.land/glamour/v2`, `github.com/charmbracelet/x/term`, and `gopkg.in/yaml.v3`. The license is MIT.

## Layout

```
cmd/co                 command, config, signals, ollama setup, session list
internal/agent         one conversation, the tool loop, the session title
internal/command       slash commands shared by both fronts
internal/openai        HTTP client and SSE chat stream (was internal/ollama)
internal/provider      config, credentials, and model selection
internal/tools         read-only filesystem tools, jailed to one root
internal/transcript    JSONL, plus resume, list, model, and title records
internal/ui            fullscreen Bubble Tea model
```

`internal/command.App` owns the catalog, the session, and the transcript. Both fronts call it. The UI talks to that app through `Director`.

## Config

The home file moved from `~/.co.yaml` to `~/.co/config.yaml`. `./.co.yaml` is still the project file.

| Source | Role |
| --- | --- |
| `--model`, or `CO_MODEL` | Model for this process. Does not change the config file. |
| `model` in `~/.co/config.yaml` | Default model. |
| `model` in `./.co.yaml` | Default model when the home file has none. |
| `providers` in `./.co.yaml` | Replaces the home provider with the same id. Other home providers stay. |
| `--config path` | That file alone, for providers and the default model. `/models` does not write the home file. |

A missing home or project file is fine. `--config` pointing at a missing file is an error. An empty file is an empty catalog. Keys still come from the environment and `~/.co/credentials.json`.

`--host` is gone from the root command. `CO_HOST` and `OLLAMA_MODEL` are no longer read there. New flags are `--resume` (`CO_RESUME`) and `--clear-scrollback` (`CO_CLEAR_SCROLLBACK`, default off). `--model` is optional. `--no-history` and `--log-level` are unchanged.

A provider block:

| Field | |
| --- | --- |
| `base_url` | Required. Trailing slashes are removed. |
| `auth` | `none`, or omitted. `none` is the only provider that takes no API key. Any other value is an error. |
| `api_key_env` | Environment variable checked before the credentials file. |
| `models` | Model ids this provider offers. Empty entries are an error. Duplicates are dropped. |
| `compat.reasoning_effort` | Send `reasoning_effort: low`. Default off. |
| `compat.reasoning_delta` | Read `reasoning` from stream chunks and show it. Default off. |

A provider id may not be empty or contain a slash or whitespace. Writes to the home file keep every other key, put `model` first, and put `providers` under it. A new file is mode `0644`. The directory is mode `0700`.

Startup no longer calls `GET /v1/models/{model}`. A bad model fails on the first chat request.

### API keys

A key is never stored in the YAML. For a provider that needs one, `co` uses `api_key_env` when that variable is set, then `~/.co/credentials.json`. The file is a JSON object of provider id to key, written through a temp file in the same directory, mode `0600`. The directory is mode `0700`.

A missing key returns `provider "<id>" has no API key: run /connect <id>`.

## Startup

`run` loads the catalog, installs the same signal handling, builds `command.App`, and enters the front end.

| Situation | What happens |
| --- | --- |
| `--resume` set | That session is opened. A bad id or a transcript that cannot be parsed is exit 2. |
| A model is selected | `Activate` opens the client. The choice is not written back. |
| No model, and both streams are terminals | The TUI opens on the model picker. Esc quits. |
| No model, otherwise | Exit 2: set `--model` or `model` in `~/.co/config.yaml`. |

An unknown provider named `ollama` adds `: run co ollama`. In a terminal that error is printed and the TUI still opens, with no session until `/models` succeeds. Otherwise the process exits.

`Activate` and `Select` both resolve `provider/model-id` to a base URL, key, compat flags, and the wire model id. `Select`, which is what `/models` calls, also writes the selection to `~/.co/config.yaml` unless `--config` was set. Switching model keeps the message list and points later turns at the new client. A change appends a model record. The session header keeps the model from `Open`.

## `co ollama`

`co ollama` needs the `ollama` binary on `PATH`. `--host` wins, then `OLLAMA_HOST`, then `http://127.0.0.1:11434`. A host without a scheme is treated as `http://`. A trailing slash is removed. An empty host is exit 2.

It probes `GET /api/ps` with a 500 ms timeout. If that fails because the daemon is unreachable, `co` starts `ollama serve` only for `127.0.0.1`, `localhost`, and `::1`, in its own session, and waits up to 10 seconds. Any other host is reported as not running.

| Daemon | Choice |
| --- | --- |
| Already up, one model loaded | That model, no prompt. |
| Already up, several loaded | Ask among the loaded models. |
| Just started, or none loaded | Ask among installed models from `GET /v1/models`. |

The prompt is a numbered list on stderr. The answer may be the number or the model id. The chosen model is added to the installed list if it was only loaded. The list is sorted and written as the `ollama` provider: `auth: none`, and both compat flags when that provider is created. Later runs replace `base_url` and `models` and leave an existing `compat` block alone. The `model` key becomes `ollama/<chosen>`.

A terminal then opens the session on that model. Anything else prints `model: ollama/<chosen>` and returns.

## Slash commands

`command.Parse` runs before a line can become a turn. `exit` and `quit` leave. `/models` and `/connect` are the only slash commands. Anything else starting with `/` is an error and does not start a turn.

| Line | Line reader | TUI |
| --- | --- | --- |
| `/models` | Prints each `provider/model-id` on stderr. | Opens the model picker. |
| `/models provider/model-id` | Selects it, prints `model <id>` on stderr. A failure is printed and the prompt returns. | Selects it. Success adds a `model <id>` note, except on the first pick at startup. |
| `/connect` | Prints providers that accept a key. Empty list: `no providers accept an API key`. | Opens that list. |
| `/connect provider` | On a terminal, prompts `API key for <id>:` with echo off, then `saved key for <id>`. Otherwise exit 2. | Masked key prompt. Enter saves. Esc returns to the composer. |
| `exit`, `quit` | Prints the resume line, when a transcript was written, and returns. | Same resume line on stderr, then quit. |

In the TUI, Tab completes a slash name when the cursor is at the end of a single line. `/` cycles through `/connect` and `/models`. Up and down move the highlight. Enter accepts the highlighted name and then submits. The line reader has no completion.

A chat line with no session tells the TUI to `select a model with /models`. The line reader exits 2.

## Sessions

The file is still `~/.co/projects/<project>/sessions/<id>.jsonl`, created on the first completed message, mode `0600`. Two record types were added. The header is not rewritten.

```json
{"type":"model","v":1,"seq":2,"ts":"RFC3339Nano","model":"ollama/qwen3:8b"}
{"type":"title","v":1,"seq":3,"ts":"RFC3339Nano","title":"Where the jail lives"}
```

Writes take one mutex. The title request runs beside the next turn, so a message write and a title write can meet.

### Resume

`--resume` takes a session id or `latest`. A trailing `.jsonl` is stripped. `latest` is the most recently modified file in this directory. Equal modification times pick the greater id. An id may contain letters, digits, `-`, `_`, and `.`.

The file is opened read-write. A trailing partial line is cut off and the file is truncated to the last complete line. The header `cwd` must be this directory. Messages are restored after a fresh system prompt and are not written again. Tool calls become tool lines. A tool result marks the matching line done. Reasoning is still not stored, so a resumed session does not show it.

`--model` or `CO_MODEL` overrides the model stored in the transcript. `--no-history` restores the messages, closes the writer, and does not append.

On `exit` or `quit`, when this process has a transcript file, stderr gets:

```
resume with: co --resume <id>
```

Ctrl+C and EOF do not print that line.

### List

`co session list` prints the newest sessions for this directory. `--limit`, `-n`, and `CO_LIMIT` set the count. The default is 20. A limit below 1, or a non-integer `CO_LIMIT`, is exit 2. A missing sessions directory prints nothing.

Columns are `ID`, `UPDATED` (UTC RFC3339), `MODEL`, and `TITLE`, separated by tabs. The first row is the session `--resume latest` would open. A transcript that cannot be parsed is skipped with a warning. The title is the latest title record. When the file has none, it is the first line of the first prompt, at most ten words. The cell is one line, and longer than 60 runes it ends in `...`.

### Title

The first user message in a new session stores that ten-word line immediately. After that turn succeeds, a background chat asks for a replacement. The request has its own two-minute timeout, no tools, and this system text: name the chat in at most six words, no quotes, no trailing punctuation, the title only. The prompt is clipped to 2000 runes.

The reply's first non-empty line is kept. Surrounding quotes and trailing `.,;:!?` are removed, then the line is cut to six words. An empty result, or one equal to the ten-word line, leaves the stored title. A failure logs a warning and leaves it too. Resume does not name the chat again, because the restored messages already contain a user turn.

`App.Close` waits for that request, then closes the file. Quitting after the first successful turn can wait up to two minutes.

## OpenAI client

`internal/openai` is the old Ollama client, pointed at any compatible endpoint.

| Base URL | Chat path |
| --- | --- |
| ends in `/v1` or `/openai` | `/chat/completions` |
| anything else, including an Ollama host | `/v1/chat/completions` |

A non-empty API key is sent as `Authorization: Bearer`. `reasoning_effort` is sent only when that compat flag is on. `reasoning` deltas are reported only when `reasoning_delta` is on. `openai.New`, used by tests, turns both on. A configured provider does not, until its YAML says so. `co ollama` does, when it creates the provider.

Tool-call assembly is unchanged, with one addition. `extra_content.google.thought_signature` is stored on the draft and sent back on the tool call. Gemini requires that signature on the call that carried it.

## TUI

The program takes the alternate screen (`AltScreen`), reports focus, and asks for cell-motion mouse events. The window title is still `co`. A focus event clears the screen, so a terminal search that scrolled an older buffer under the frame is painted over again.

`--clear-scrollback` erases the display before the program starts. When `TERM_PROGRAM` is `iTerm.app` it also sends iTerm's clear-scrollback sequence. iTerm's Find searches that buffer, and a hit from before this process scrolls it into view under the frame.

The transcript is a viewport with soft wrap, pinned to the bottom until the user scrolls up. The status line then says `pgdn to follow`. The mouse wheel and trackpad move one line. A wheel event over the composer goes to the textarea. PgUp and PgDn move a page. When the transcript is taller than the viewport, a one-column bar is drawn on the right: `┃` for the thumb, `│` for the track.

The composer is one rounded box drawn outside the textarea. The border is teal while focused and zinc while not. The placeholder is `what do you want to cook`. Height is two to six lines. Enter sends. Ctrl+J, Alt+Enter, and Shift+Enter insert a newline. Ctrl+C quits, or cancels the turn while one is running. Ctrl+D quits when the input is empty and the session is idle.

### What a block looks like

| Block | |
| --- | --- |
| User | Teal bar, `❯`, wrapped plain text. |
| Assistant | Glamour markdown, inset two columns. Dark or light style follows the terminal background. |
| Reasoning | A `thinking` label, then the same markdown in the faint color, without syntax colors. |
| Tool | `→` while running, `✓` when done, `✗` on failure. Names are `List`, `Read`, and `Grep`. |
| Note | Dim `·` line. Used for `interrupted`, the model switch, and a saved key. |
| Error | Rose bar. |

An empty transcript says `Ask about this directory. Tools are read-only.` Markdown that fails to render is wrapped as plain text. A resumed session paints user, assistant, and tool blocks from the restored messages before the first paint, and the viewport starts at the bottom.

### Pickers

`/models` and `/connect` share one list. It shows eight rows, centered on the cursor. Typing filters by a case-insensitive substring. Backspace edits the filter. Up, down, and the wheel move the cursor. Enter selects. Esc closes the list, or quits when no session exists yet. Ctrl+C quits.

The model list is the configured `provider/model-id` values, sorted by provider id. The connect list is providers that accept a key. The key itself is shown as `•` per rune. Paste is accepted. Newlines, tabs, and surrounding space are stripped. An empty key stays on the prompt. A saved key adds `saved key for <id>`.

While a select or a save is in flight, only Ctrl+C is handled.

The help line follows the mode: send and scroll while chatting, stop and scroll while busy, move and complete while a slash command is open, select or quit in a picker, save or cancel while entering a key.

## Build

Push and pull request each run two workflows, `contents: read`, on `ubuntu-latest`, with Go from `go.mod`.

| Workflow | Step |
| --- | --- |
| `test` | `go test ./...` |
| `lint` | `golangci-lint` v2.15.0. Standard linters, `gofmt`, and the `std-error-handling` exclusion. |

`.gitignore` ignores `.agents/`, `.crush/`, `.idea/`, `.vscode/`, and `.DS_Store`.

## Tests

`go test ./...` covers provider merge and precedence, credential writes, YAML field order, resume and list parsing, title cleanup, slash-command completion, the Ollama host and model prompt, and TUI updates for markdown, resume, pickers, connect, tab completion, and one-line wheel scrolling.
