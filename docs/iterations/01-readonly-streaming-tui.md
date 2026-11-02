# 01 — Read-only streaming TUI

Baseline commit: `2db263a3da4186384ccfab80bb7e336b3aac46cf` (`2db263a`)

| | |
| --- | --- |
| Subject | show streamed reasoning in the tui |
| Author | kevinjom |
| Date | 2026-10-27 21:16:08 +0800 |
| Branch | `main` |

This file describes the program at that commit. The next iteration starts there and covers only what landed afterward:

```
git log --oneline 2db263a3da4186384ccfab80bb7e336b3aac46cf..HEAD
git diff 2db263a3da4186384ccfab80bb7e336b3aac46cf..HEAD
```

## What co is

`co` is a coding assistant for the directory you start it in. It talks to one Ollama daemon over the OpenAI-compatible HTTP API. The model may call three tools — `list_dir`, `read_file`, and `grep` — and every path is resolved and rejected if it leaves that directory.

A terminal on both stdin and stdout gets a Bubble Tea TUI. Anything else (a pipe, a redirect) gets a line reader. Both fronts share one in-memory `agent.Session`.

Module path is `co`, Go 1.26. Direct dependencies are cobra, viper, Bubble Tea v2, Bubbles v2, Lip Gloss v2, and `github.com/charmbracelet/x/ansi`.

## Layout

```
cmd/co                 command, config, signals, which front to run
internal/agent         one conversation, the tool loop, turn events
internal/ollama        HTTP client and SSE chat stream
internal/tools         read-only filesystem tools, jailed to one root
internal/transcript    one JSONL file per session
internal/ui            Bubble Tea model and rendering
```

## Startup

`main` calls `Execute`, which resets viper, builds the root command, and runs it with `context.Background()`.

`PersistentPreRunE` loads config. `RunE` (`run`) checks the model, checks the host, installs signal handling, asks Ollama whether the model exists, opens the working directory, opens a transcript, and enters the front end.

### Config

Precedence is the usual viper order: flag, then environment, then file, then default.

| Input | Names |
| --- | --- |
| Flags | `--config`, `--log-level`, `--host`, `--model`, `--no-history` |
| Environment | `CO_HOST` and `OLLAMA_HOST`; `CO_MODEL` and `OLLAMA_MODEL`; `CO_LOG_LEVEL`; `CO_NO_HISTORY`. The `CO_` prefix maps `-` to `_`. |
| File | `--config`, otherwise `$HOME/.co.yaml` and `./.co.yaml` |
| Defaults | host `http://127.0.0.1:11434`, log level `info` |

`--model` is required. An empty host is a usage error. A missing config file is fine; a present but unreadable file is not. Log lines go to stderr through `slog`.

`co version` prints the ldflag values `version`, `commit`, and `date` (defaults `dev`, `unknown`, `unknown`) plus the Go version from build info.

### Exit codes

| Code | When |
| --- | --- |
| 0 | clean exit, including `exit`, `quit`, EOF, and Ctrl+C while idle in the TUI |
| 1 | any other error |
| 2 | usage: missing model, empty host, bad log level |
| 130 | interrupt |
| 143 | SIGTERM |

`startSignals` cancels a child context on interrupt and SIGTERM. The process exit code comes from that signal when the context ends. The TUI is started with `tea.WithoutSignalHandler`, so this package owns the signals.

### Model check and history

`client.Show` sends `GET /v1/models/{model}`. A failure aborts before the prompt. A signal during that call returns the signal's exit error.

History is on unless `--no-history` is set. `transcript.Open` prepares a path under the home directory. If the home directory or the open fails, the process logs a warning and continues with a nil recorder.

## Two fronts

`repl` calls `ui.Run` when both stdin and stdout are character devices. Otherwise it runs `lineRepl`.

The line reader prints `> ` on stderr when stdin is a terminal. Blank lines are skipped. `exit` and `quit` return. Each other line is one `Session.Turn`.

| Event | Where it goes |
| --- | --- |
| `AnswerDelta` | stdout, with no extra separator |
| `ReasoningDelta` | stderr |
| `ToolStart` | stderr, `tool <name> <detail>` |
| `ToolDone` with an error | stderr, `tool <id> error: ...` |

After a streamed answer, the line reader adds one newline. If the model returned text without any answer delta, that text is printed as a whole line. The scanner allows a 1 MiB line. Reading waits on a goroutine so a canceled context can return while `Scan` is still blocked on the next line.

## Agent loop

`Session` holds the client, the model name, the tool set, the tool definitions, the message list, and an optional `Recorder`. It starts with one system message:

- the assistant is `co` and is read-only
- the working directory is named
- answers come from tools
- file contents and line counts are taken from tool results
- a tool error is reported
- a finished answer is plain text and stops calling tools

`Turn` appends the user line, then calls the model.

1. Up to 8 rounds (`maxToolRounds`) run `ChatStream` with the tool definitions.
2. A message with no tool calls is the answer.
3. Otherwise each call runs, and a `role: tool` message is appended with the tool text and the call id.
4. If the model is still calling tools after those rounds, one more `ChatStream` is made with the tools omitted. Its content is returned. Calls on that last message are stored and not executed.

`complete` forwards stream chunks:

- content becomes `AnswerDelta`
- reasoning becomes `ReasoningDelta` and is also buffered

`answerText` stores the model's content when that content is non-empty or the message has tool calls. When both are empty, the buffered reasoning is stored as the assistant content. That covers models (Qwen among them) that stream the whole reply on the reasoning channel.

### Cancellation and history

A model error leaves the user line in the session and drops the partial assistant text, except when the context was canceled or its deadline was exceeded and some content had already arrived. In that case tool calls are cleared and the partial assistant message is kept, marked interrupted, so the next turn can see what was already shown.

`remember` appends to the in-memory list and, when a recorder is set, writes the completed message. A write error logs a warning, sets the recorder to nil, and leaves the turn successful. Streaming deltas are not recorded.

### Events

The set is closed: `ReasoningDelta`, `AnswerDelta`, `ToolStart`, `ToolDone`. `emit` panics on any other type, and so do the switches in the line reader and the TUI. A nil observer is ignored.

`ToolStart.Detail` is `pattern="..."` for `grep` and `path=...` for the others (`.` when the path is empty). `ToolDone.Err` is set when the tool failed. The model still receives that failure as the tool message text (`error: ...`).

## Ollama

`Client` is a small HTTP client. The base URL is trimmed of trailing slashes. Responses are capped at 8 MiB.

`ChatStream` posts `/v1/chat/completions` with `stream: true` and `reasoning_effort: "low"`. The body is server-sent events. Lines other than `data:` are skipped. `data: [DONE]` finishes the message.

Each chunk's first choice is applied:

- a non-empty `delta.content` is appended and reported as `StreamContent`
- a non-empty `delta.reasoning` is reported as `StreamReasoning` and is not kept on the client message
- `delta.tool_calls` are merged into drafts

Tool-call assembly uses `index` when the chunk has one. Ollama often omits it and sends a whole call in one chunk, so a new id or name starts another draft, and a bare arguments fragment extends the last draft. Empty ids become `call_1`, `call_2`, …. Empty types become `function`. The concatenated arguments string is JSON-decoded into a map; a blank string is an empty map.

`Arguments` marshals as a JSON string, which is the OpenAI chat shape, and unmarshals from either that string or a raw object.

A canceled or deadline-exceeded stream returns the text received so far and drops tool calls, because their arguments may be incomplete. An empty body is an error. A non-200 response includes the status and body.

## Tools

`tools.New(cwd)` builds `list_dir`, `read_file`, and `grep`. `Run` returns the result string and a nil error on success. On failure it returns both `error: ...` and the error, so the model can recover and the UI can mark the line.

`resolve` is the jail. It takes the absolute, symlink-resolved working directory and the absolute, symlink-resolved candidate, then `filepath.Rel`. A result of `..`, a path under `..`, or an absolute relative path is rejected as outside the working directory.

| Tool | Arguments | Result |
| --- | --- | --- |
| `list_dir` | `path`, default `.` | `entries: N`, then `dir` or `file` and the name, one directory level, sorted by name |
| `read_file` | `path` required; `offset` default 1; `limit` default 200 | `lines: N` is the total line count, then `N\|text` for the window. The window stops at 32 KiB and then says `truncated: true` |
| `grep` | `pattern` required (Go regexp); `path` default `.` | `matches: N`, then `path:line:text`, at most 50. A cap adds `truncated: true` |

`grep` walks directories, skips `.git`, and reads regular files. A start path whose base name is `.git` returns no matches. Unreadable files and walk errors are skipped. A line longer than 1 MiB fails the read. `read_file` uses the same line limit.

## Transcript

`Open(home, cwd, model)` prepares `~/.co/projects/<project>/sessions/<id>.jsonl`. The file is created on the first `Record`, so a session that never stores a message leaves nothing behind.

`<project>` is the absolute, symlink-resolved working directory with every separator replaced by `-`. `<id>` is UTC `20060102T150405Z` plus two random bytes in hex.

The sessions directory is mode `0700`. The file is mode `0600`, opened append-only. The first line is a header; each later line is one completed message. Each line is a single `Write`.

Header:

```json
{"type":"session","v":1,"id":"...","cwd":"...","model":"...","started":"RFC3339"}
```

Message:

```json
{"type":"message","v":1,"seq":1,"ts":"RFC3339Nano","interrupted":false,"message":{"role":"user","content":"..."}}
```

`interrupted` is true for an assistant message cut short by cancellation. A failure while writing the header closes and removes the file. `Close` before any `Record` is a no-op.

## TUI

`ui.Run` builds a `model` and runs it inline, which is Bubble Tea's default: `View` is redrawn in place, and the transcript scrolls inside a viewport. The window title is `co`.

The screen is a header, the viewport, a textarea, a status line, and a help line. The header shows `co`, the model, and the working directory with `$HOME` shortened to `~`. The textarea is two to six lines, placeholder `Ask about this directory`, limit 64 KiB. The theme follows `tea.BackgroundColorMsg`: teal accent, zinc dim text, rose for failures.

| Key | Idle | Busy |
| --- | --- | --- |
| Enter | send the trimmed input | ignored |
| Ctrl+J, Alt+Enter, Shift+Enter | insert a newline | |
| PgUp / PgDn | scroll the viewport | scroll the viewport |
| Ctrl+C | quit | cancel the turn |
| Ctrl+D | quit when the input is empty | ignored |
| `exit` or `quit` as the whole input | quit | |

An empty Enter does nothing.

### A turn on screen

Submit clears the input, appends a user block, and runs `Session.Turn` on a goroutine with a child context. Events arrive on a buffered channel (256) as `eventMsg`. `turnDoneMsg` closes the turn.

Deltas are coalesced onto one block: reasoning onto a dim reasoning block, answer text onto an assistant block. A tool start closes those blocks, sets the status to the tool name and detail, and appends a tool line. `ToolDone` with an error paints that line in the failure color.

Paint is throttled to 30 Hz. The viewport stays pinned to the bottom until the user scrolls up; the status line then says `pgdn to follow`. While busy the status is the spinner plus `thinking`, the tool detail, or `answering`. While idle it says `read-only`. The help line switches to `ctrl+c stop` during a turn. Bubble Tea's program status is `Working` while busy and `Blocked` (waiting for a prompt) while idle.

When the turn finishes:

- a success that already streamed an answer, or that already showed reasoning, adds nothing further
- a success with text and no streamed answer or reasoning adds one assistant block
- cancel or deadline adds a dim `interrupted` note
- any other error adds a failure block

A stale `turnID` is ignored, so a canceled turn cannot paint over the next one. If the parent signal context is already canceled when the turn finishes, the program quits with that error.

## Tests

`go test ./...` covers the loop (tool round cap, partial answers on cancel, history failures, reasoning-only replies), the SSE parser (split tool calls, missing indexes, cancel, empty body), the path jail, read and grep limits, the JSONL record shape, TUI updates (streaming, interrupt, stale turns), and a piped CLI turn that separates the answer on stdout from the tool trace on stderr.
