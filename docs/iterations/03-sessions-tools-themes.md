# 03 — Sessions, language tools, and themes

Baseline commit: `9a1897f9ed3643b656a1c7d6f87e29e824555ddf` (`9a1897f`)

| | |
| --- | --- |
| Subject | feat: @ to trigger fuzzy search files |
| Author | kevinjom |
| Date | 2026-11-04 22:40:24 +0800 |
| Branch | `main` |

This file describes what changed after [02](02-providers-sessions-fullscreen.md). The path jail, the event kinds, the signal exit codes, the OpenAI path rules, the `thought_signature` round-trip, the transcript record types, and the credentials file are unchanged. The next iteration starts here:

```
git log --oneline 9a1897f9ed3643b656a1c7d6f87e29e824555ddf..HEAD
git diff 9a1897f9ed3643b656a1c7d6f87e29e824555ddf..HEAD
```

## What co is now

`co` still talks to one chat endpoint per turn. The model may list, read, and search inside the working directory, and it may also search the web, fetch one http or https page, and ask a language server about the tree. It still cannot write files. A language-server result may name a file outside the working directory, and it does not include that file's body.

The TUI can hold more than one session. A pipe or a redirect still gets the line reader, and that reader still has one session.

The module path is `github.com/kevinjom/co`, Go 1.26.4. Direct dependencies add `github.com/PuerkitoBio/goquery`, `github.com/charmbracelet/ultraviolet`, `github.com/dotcommander/defuddle`, `github.com/sabhiram/go-gitignore`, `github.com/sahilm/fuzzy`, and `github.com/sourcegraph/jsonrpc2`. The license is MIT. The README says the program is under active development and that the interfaces, the behavior, and the on-disk config may change.

## Layout

```
cmd/co                 command, config, signals, ollama setup, session list
internal/agent         one conversation, the tool loop, the session title
internal/command       slash commands shared by both fronts
internal/config        resolved flags: level, theme, tool rounds, retention
internal/logs          daily log files under ~/.co/logs
internal/lsp           read-only language-server client, one process per binary
internal/openai        HTTP client and SSE chat stream
internal/provider      config, credentials, and model selection
internal/theme         named palettes, each with a light side and a dark side
internal/tools         one file per tool: filesystem, web, and language server
internal/transcript    JSONL, plus resume, list, model, and title records
internal/ui            fullscreen Bubble Tea model
```

`internal/command.App` still owns the catalog and the sessions. It also owns one language-server hub and shares it with every session. `App.Close` waits for titles, closes every transcript, then shuts the servers down.

## Config

Settings and providers are separate files. `~/.co/config.yaml` holds the default model and the flags that are safe to keep. `~/.co/models.yaml` holds the providers. A provider block that is still sitting in `config.yaml` is not read.

| Source | Role |
| --- | --- |
| `--model`, or `CO_MODEL` | Model for this process. Does not change the config file. |
| `model` in `~/.co/config.yaml` | Default model. |
| `model` in `./.co.yaml` | Default model when the home file has none. |
| `providers` in `~/.co/models.yaml` | Home providers. |
| `providers` in `./.co.yaml` | Replaces the home provider with the same id. Other home providers stay. |
| `theme`, `log-level`, `log-retention-days`, `no-history`, `clear-scrollback`, `max-tool-rounds` in `~/.co/config.yaml` | Defaults for those settings. |
| `--config path` | That file alone, for flags, providers, and the default model. `/models` and `/theme` do not write the home file. |

`resume` is still a one-shot. A value in the settings file is dropped unless this process passed `--resume` or `CO_RESUME`.

`co ollama` writes the provider to `~/.co/models.yaml` and the chosen model to `~/.co/config.yaml`. With `--config`, both land in that one file, model first and providers under it. An existing `compat` block is still left alone. The probe, the local-only daemon start, and the numbered prompt are unchanged.

## Logs

Log lines go to `~/.co/logs/co-YYYY-MM-DD.log`, using the local date. The file is mode `0600` and the directory is mode `0700`. The file is created on the first line.

`log-retention-days` is how many of those daily files to keep, including today. The default is 3. There is no flag. `CO_LOG_RETENTION_DAYS` and the settings file set it. A value below 1 is exit 2. A file from an older day is removed when `co` starts. After midnight, the next log line opens that day's file and removes files outside the window.

`log-level` is still `debug`, `info`, `warn`, or `error`, default `info`.

## Tool rounds

A turn may include tools on `max-tool-rounds` model calls. The default is 64, up from a fixed 8. The next call is made with no tools. `--max-tool-rounds`, `CO_MAX_TOOL_ROUNDS`, and `max-tool-rounds` in the settings file set it. A value below 1 is exit 2. Each session receives the process value. A later `SetMaxToolRounds` below 1 leaves the current value.

The system prompt tells the model to use `web_search` when the answer is not in this directory, and `web_fetch` to read one page. When language tools are on, it also tells the model to prefer them over `grep` for definitions, references, and diagnostics.

## Sessions

The TUI keeps every session opened in this process. Each one has its own transcript, its own message list, and its own turn. The line reader still uses the first session only.

| Line | Line reader | TUI |
| --- | --- | --- |
| `/new` | Prints `available in the TUI`. | Opens another session on the current model and shows it. Other sessions keep running. |
| `/sessions` | Prints `available in the TUI`. | Opens a picker of the sessions in this TUI. Typing filters titles. Enter switches. |
| `/models provider/model-id` | Retargets the only session. | Retargets the session on screen. Later `/new` uses that model. Sessions left in the background keep their client. |
| `exit`, `quit` | One resume line per transcript this process wrote. | The same lines on stderr. |

A session with no model yet still says `select a model with /models`. `/new` says the same until the first session exists.

The file is still `~/.co/projects/<project>/sessions/<id>.jsonl`, created on the first completed message, mode `0600`. `co session list` is the on-disk list. The panel on the right is only the sessions opened in this TUI.

Switching sessions keeps each one's draft, scroll offset, and whether it was pinned to the bottom. The header shows the process model and the short path of the working directory.

### The panel

When the terminal is at least 60 columns wide and a session exists, a 24-column list sits to the right of the transcript. The heading is `sessions`. Each row is a status icon and the title. The focused row uses the accent color. The list is centered on that row when it is taller than the viewport.

| State | Icon |
| --- | --- |
| New, no user message yet | `○` |
| Working | The spinner |
| The last turn finished | `●` |
| The turn was stopped | `–` |
| The turn failed | `✕` |

### Title

The ten-word fallback is stored when the first prompt is sent, before the reply. The background naming request starts then, beside the turn, and uses only that prompt. The request is otherwise the same: two minutes, no tools, at most six words, the first prompt clipped to 2000 runes. The panel updates when the name changes. Resume still does not name the chat again. `App.Close` waits for every session's request, so quitting after a first prompt can wait up to two minutes per session that is still naming.

### A turn that is not on screen

Each turn pushes events into its own pipe. The push does not wait for a paint, and adjacent answer or reasoning deltas are joined. The UI stays subscribed to a background turn, so its blocks keep growing. Paint runs for the session on screen, at most 30 times a second. The spinner keeps ticking while any session is working. The window status is `working`, or the focused session's activity when that session is the busy one. Idle, it is `waiting for a prompt`.

Ctrl+C stops the turn on screen. Other sessions keep going. A draft in the box is cleared first. The next Ctrl+C, with the box empty, drops the queue and cancels the turn. Ctrl+C while the session on screen is idle quits. A drag selection takes the first Ctrl+C and copies it. That does not stop the turn and does not quit.

## Steering

While the session on screen is busy, Enter appends the line to that session's queue and clears the box. The lines sit above the box, oldest first, each as `·` plus the text on one line. When they do not fit, the newest are shown and a dim row says how many are hidden.

Esc, with the box empty, pulls the newest line back into the box. Esc does nothing to the queue when the box already has text.

When the turn finishes, every line still queued is sent as one follow-up. Each line becomes its own user message, and the model answers them in one tool loop. A stop drops the lines that were waiting.

The help line follows that. Idle: send, newline, drag to copy, scroll, quit. Busy with an empty box: queue, newline, stop. Busy with a queue: queue, recall, stop. Busy with a draft: queue, or Ctrl+C to clear the draft.

## Slash commands

`/models` and `/connect` are unchanged. `/new`, `/sessions`, and `/theme` were added. Tab completes all five when the cursor is at the end of a single line. `/` cycles `/models`, `/connect`, `/new`, `/sessions`, `/theme`. Up and down move the highlight. Enter accepts the highlighted name and then submits. The file list, when it is open, takes Up, Down, Tab, and Enter before command completion does.

| Line | Line reader | TUI |
| --- | --- | --- |
| `/theme` | Prints each palette name on stderr. | Opens the theme picker. |
| `/theme name` | Saves it and prints `theme <name>`. A bad name is printed and the prompt returns. | Applies it, saves it, and adds a `theme <name>` note. |

The choice is written to `~/.co/config.yaml` unless `--config` was set. An isolated catalog still rejects a bad name, and it does not write the home file. The TUI applies the palette for this process either way.

## Language servers

`co` speaks the Language Server Protocol as a client. The details are in [the language server page](../../site/src/content/docs/tools/lsp.md). The short form:

The tools are registered only when at least one server binary is on `PATH`. If none is, `co` logs `language tools disabled` and the model keeps `grep`. CI does not install `gopls`. The only row shipped is `gopls` with no arguments, for `.go` files. The process starts on the first call that needs it, not at startup. Its working directory is the directory `co` was started in. The process is not jailed. `gopls` may read the module cache. Tool results do not include the body of a file outside the working directory.

| Tool | |
| --- | --- |
| `lsp_definition` | Definition at `path`, `line`, `column`. |
| `lsp_references` | References, including the declaration. |
| `lsp_hover` | Hover text. Longer than 8192 code points, it is cut. |
| `lsp_symbols` | Symbols in one file (`path`) or a workspace search (`query`). One of the two. |
| `lsp_diagnostics` | One file, or the workspace when `path` is omitted. Pull diagnostics only. |

Line and column start at 1. The column counts Unicode code points. Results are capped at 50, and a capped result says `truncated: true`. A location inside the working directory is a relative path. A location outside it is an absolute path, the server's line and character, and the words `outside the working directory`.

Before a request about a file, `co` reads that file from disk. The first time, it sends `textDocument/didOpen` with the whole file. If the bytes later differ, it sends one `textDocument/didChange` that replaces the whole file. Canceling a turn sends `$/cancelRequest` for the call in flight. The server process keeps running.

The client asks for position encodings `utf-8` then `utf-16`, and uses whichever the server picks. It does not advertise `workspace.applyEdit`. Rename, formatting, code actions, and applying edits are not tools.

In the transcript, a position tool is shown as `path:line:column`. `lsp_symbols` with a query is `query="..."`. `lsp_diagnostics` with no path is `workspace`.

## Web

`web_search` and `web_fetch` are always registered. They are not jailed to the working directory.

`web_search` posts the query to `https://lite.duckduckgo.com/lite/`. The default is 8 results, and the cap is 10. A query longer than 400 runes is rejected. Sponsored rows are skipped. A DuckDuckGo redirect is unwrapped. The text is `results: N`, then each hit as a number, a title, a URL, and a snippet.

The client timeout is 30 seconds. The HTML body is refused past 2 MiB. Status 202 and 429 are retried, up to three attempts, 300 ms apart. Any other status is an error.

`web_fetch` takes one absolute http or https URL and returns the page's main content as Markdown. The result starts with `url`, `title` when the page has one, and `words`. The body is cut at 32 KiB on a rune boundary, and a cut result says `truncated: true`.

The transcript shows `query="..."` for a search and the URL for a fetch.

## Themes

`--theme`, `CO_THEME`, and `theme` in the settings file choose the palette. The default is `teal`. The names are `teal`, `amber`, `rose`, `sky`, `violet`, and `ink`. A bad name is exit 2.

Each palette has a light side and a dark side. The TUI asks the terminal for its background and picks the side. Neutrals stay zinc. Good stays green. Warn and bad move only when they would match the accent. The composer border uses the accent while focused and the blur color while not.

A focus event still clears the screen. `--clear-scrollback` is unchanged.

## TUI

The program still takes the alternate screen, reports focus, and asks for cell-motion mouse events. The window title is still `co`. The transcript is still a viewport with soft wrap, pinned to the bottom until the user scrolls up. The wheel and trackpad move one line. PgUp and PgDn move a page. The scrollbar is unchanged. The status line still says `pgdn to follow` when the transcript is scrolled up.

### Copy

Cell-motion mouse mode takes the drag away from the terminal, so the program copies the cells itself. A drag that leaves the starting cell is a selection. Releasing it copies the text. The copy goes out as OSC 52, and on macOS it is also written with `pbcopy`, which covers Terminal.app. The selection stays highlighted, and the status line says `copied`, until the next key or wheel. Ctrl+C while a selection exists copies it again.

### An empty transcript

An empty transcript shows the mark from `docs/logo.svg` when the column is at least 20 cells wide, centered in the viewport, with the caption under it. The mark is a purple C and a mint ring, drawn with half blocks, and those two colors stay put when the theme changes. A narrow column shows only the caption: `Ask about this directory. Tools are read-only.` and the hint that `@` finds a file. Markdown that fails to render is still wrapped as plain text.

### `@` files

The TUI starts a walk of the working directory when it opens. The walk keeps regular files, skips `.git`, and skips paths excluded by a `.gitignore` along the way, including nested files. It stops after 20,000 files. The list is sorted.

Typing `@` opens the list. The text after `@` is the query. An empty query shows the sorted paths. Any other query is ranked by `sahilm/fuzzy`, which prefers consecutive characters and a match after a path separator. The matched characters are drawn in the accent. The list shows eight rows, centered on the cursor. Up and down move. Tab or Enter replaces the token with `@path` and leaves the cursor after a following space, so the list does not reopen on the path. Esc hides the list for that token until the token changes. A token that does not start with `@`, such as `name@host`, is left alone.

While the walk is still running the list says `indexing files`. An empty tree says `no files`. A query with no hit says `no matching file`.

### What a block looks like

User, assistant, reasoning, note, and error blocks are unchanged. Tool names `list_dir`, `read_file`, and `grep` are still `List`, `Read`, and `Grep`. A web or language-server tool is shown under its own name. The status line names the activity: `thinking`, `answering`, or the tool label plus its detail.

## Build

`make build` writes `./co` with the version, commit, and date ldflags. Unset, those are `dev`, the short HEAD, and the UTC build time. `make test` is `go test ./...`. `make lint` is `golangci-lint run`. The built binary is gitignored.

Push and pull request each run the same two workflows as before, `contents: read`, on `ubuntu-latest`, with Go from `go.mod`. Lint is still `golangci-lint` v2.15.0.

`docs/architecture.excalidraw` is a diagram of the process: the two fronts, the settings file and the provider file, the session loop, and the jailed filesystem tools. Language servers and web tools landed after that drawing. `docs/lsp.md` is the language-server note. `docs/logo.svg` is the mark.

## Tests

`go test ./...` covers the settings split and theme writes, log rotation and retention, the tool-round cap, title timing, several sessions at once, slash commands for `/new`, `/sessions`, and `/theme`, the language-server client against a stub, web search parsing and fetch clipping, palette names, the session panel, the steering queue, background turns, drag copy, and the `@` file list.
