# 04 — Writes and skills

Baseline commit: `f0999e064bbd2e500da8442ed41b7ff778f12394` (`f0999e0`)

| | |
| --- | --- |
| Subject | Merge pull request #4 from kevinjom/feat/skills |
| Author | Kevin Fan |
| Date | 2026-11-07 15:50:09 +0800 |
| Branch | `main` |

This file describes what changed after [03](03-sessions-tools-themes.md). The event kinds, the signal exit codes, the OpenAI path rules, the `thought_signature` round-trip, and the credentials file are unchanged. The path jail now also covers creating a file. The next iteration starts here:

```
git log --oneline f0999e064bbd2e500da8442ed41b7ff778f12394..HEAD
git diff f0999e064bbd2e500da8442ed41b7ff778f12394..HEAD
```

## What co is now

`co` still talks to one chat endpoint per turn. The model may list, read, search, create, and edit files inside the working directory. It may still search the web, fetch one http or https page, and ask a language server about the tree. It still cannot leave the directory, including through a symlink. A language-server result may still name a file outside the working directory, and it does not include that file's body.

The TUI can hold more than one session. A pipe or a redirect still gets the line reader, and that reader still has one session.

The module path is `github.com/kevinjom/co`, Go 1.26.4. Direct dependencies are unchanged. The license is MIT. The README says the program is under active development and that the interfaces, the behavior, and the on-disk config may change.

## Layout

```
cmd/co                 command, config, signals, ollama setup, session list
internal/agent         one conversation, the tool loop, the prompt, skills for one turn
internal/command       slash commands shared by both fronts
internal/config        resolved flags: level, theme, tool rounds, retention
internal/logs          daily log files under ~/.co/logs
internal/lsp           read-only language-server client, one process per binary
internal/openai        HTTP client and SSE chat stream
internal/provider      config, credentials, and model selection
internal/skill         SKILL.md files from the project and from the home directory
internal/theme         named palettes, each with a light side and a dark side
internal/tools         one file per tool: filesystem, web, and language server
internal/transcript    JSONL, plus resume, list, model, title, and skill records
internal/ui            fullscreen Bubble Tea model
```

`internal/command.App` still owns the catalog and the sessions, and it still shares one language-server hub with every session. It also loads the skill catalog once, at startup, and gives that same catalog to every session. `App.Close` waits for titles, closes every transcript, then shuts the servers down.

## Creating and editing files

`create_file` and `edit_file` are always registered. They use the same working directory as `list_dir`.

| Tool | |
| --- | --- |
| `create_file` | New text file. `path` and `contents` are required. Fails if the path already exists. |
| `edit_file` | Replace one exact snippet. `path`, `old_string`, and `new_string` are required. `old_string` must be non-empty and appear once. An empty `new_string` deletes the snippet. |

`create_file` answers `created <path> (N bytes)`. `edit_file` answers `edited <path>`. In the transcript a call is shown as `Create` or `Edit`, with `path=` and the path.

Both refuse a path that leaves the working directory. `resolve` still evaluates symlinks and rejects `..`. `resolveCreate` is the path for a file that does not exist yet. It walks each component, follows a symlink that stays inside the directory, and rejects one that does not. Missing parent directories are created mode `0755`. The file itself is not created by the walk. An existing file, an existing directory, or `.` is an error, and nothing is written.

The bytes go to a temp file in the destination directory, named `.co-write-*`. `create_file` links that temp name onto the destination, so a path that appeared in the meantime is left untouched. `edit_file` renames the temp onto the destination. A new file is mode `0644`. An edit keeps the mode the file already had. A symlink inside the directory is edited at its target, and the symlink stays a symlink.

`contents`, the file being edited, and the file after the replacement are capped at 1 MiB. A directory, a non-regular file, a missing file, and a snippet that matches zero or more than one time are errors. The model still receives the failure as the tool message text.

The language-server client is unchanged. It still does not advertise `workspace.applyEdit`. The next request about a file reads the bytes from disk, so a file this process just wrote is what the server sees. Rename, formatting, and code actions are not tools.

## The system prompt

The assistant is still `co`. The prompt no longer says the assistant is read-only.

It tells the model to create a missing file with `create_file` and to change an existing file with `edit_file`, after reading it. `old_string` must appear once, and bytes outside that snippet stay as they are. After `edit_file` succeeds, the model describes the change from the snippet it wrote and does not read the file again to confirm it. It writes simple and clean code.

When the user names a file, the model reads that file and does the work there. It opens another file only when the named file is not enough, such as its existing test. A non-technical question is turned away, toward ChatGPT or Google. The rules about invented file contents, `web_search`, `web_fetch`, tool errors, and a plain-text answer are unchanged. When language tools are on, the prompt still prefers them over `grep` for definitions, references, and diagnostics.

## Skills

A skill is a directory with a `SKILL.md` file, as in the [Agent Skills specification](https://agentskills.io/specification). `co` does not run scripts from a skill. `allowed-tools` in the frontmatter is accepted and ignored.

### Where they are read

`App` loads them when it starts. A missing root is empty. A directory that cannot be read is logged and skipped.

| Root | |
| --- | --- |
| `<working directory>/.agents/skills/<name>/SKILL.md` | Project skill. The same name replaces the home skill. |
| `~/.co/skills/<name>/SKILL.md` | Home skill. Kept when the project has no skill of that name. |

The name in the frontmatter must match the directory. It is 1 to 64 characters, from `a-z`, `0-9`, and `-`, with no leading or trailing hyphen and no `--`. The description is required and at most 1024 characters. A name that shadows a command is skipped: `models`, `connect`, `new`, `sessions`, `theme`, `skill`. A file with no frontmatter, a bad name, or an empty description is logged and skipped. The catalog is sorted by name. Adding a skill while `co` is running does not appear until the next process.

The frontmatter sits between `---` lines. A leading BOM is stripped and CRLF is treated as LF. The stored body is the whole file, including the frontmatter. A project skill remembers its directory as `.agents/skills/<name>`. A home skill has no directory.

### A turn that names one

The system prompt lists every loaded skill as `name: description` on one line, and says that a message naming `/name` follows that skill for that turn only.

`/name` and `/skill name` are the same mention. The name is a field that starts with `/` and contains no further slash. Each distinct skill is taken in first-seen order. A field that is not a loaded skill is left in the user message and does not activate anything. A line that does not start with `/` can still name a skill later in the text.

For that turn, the prompt appends one block per active skill. A project skill says its files are in `.agents/skills/<name>/` and then the whole `SKILL.md`. A home skill says the skill is active and then the file, with no path. The user message stored in the session is the line as typed.

The block lasts until the turn returns, including when the turn is canceled. The next turn sees the catalog again and not the body. Tool rounds inside the turn still see the block.

### Slash commands

`/models`, `/connect`, `/new`, `/sessions`, and `/theme` are unchanged. `/skill` was added. A known `/name` is a chat turn, not a command. An unknown `/name` is `unknown command`. `/skill` with a first word that is not a loaded skill is `unknown skill`.

| Line | Line reader | TUI |
| --- | --- | --- |
| `/skill` | Prints `name: description` on stderr, or `no skills`. | Opens a picker. Typing filters names. Enter inserts `/name` and leaves the cursor after a space. |
| `/name` | One chat turn. The skill applies to that turn. | The same, once the line is sent. |
| `/skill name …` | One chat turn when `name` is loaded. | The same. |

In the TUI, Tab completes slash commands when the cursor is at the end of a single line. Typing `/` lists them in two groups, `commands` and `skills`. The headings are not selectable. Up and down move through the names. The command group is `/models`, `/connect`, `/new`, `/sessions`, `/theme`, and `/skill`. The skills group is one `/name` for each skill, in name order. Enter on a command completes it and runs it. Tab on a command completes it and leaves it in the box. Tab or Enter on a skill inserts `/name` and a trailing space, the same way `@` inserts a file, so the rest of the turn can be typed before it is sent. The file list, when it is open, takes those keys first.

The skill picker says `choose a skill`. An empty catalog says `no skills`. A filter with no hit says `no matching skill`. Esc closes it, or quits when no session exists yet.

### The transcript

A turn does not write which skill it used. The user line is the record of the mention.

The file can also hold a skill record. Resume reads the names, in order, and does not put the skill back into the system prompt.

```json
{"type":"skill","v":1,"seq":1,"ts":"RFC3339Nano","name":"grilling"}
```

An empty name is an error. The header, message, model, and title records are unchanged.

## TUI

The welcome caption is `Ask about this directory. Tools can create and edit files here.` The mark and the `@` hint are unchanged. While idle and pinned to the bottom, the status line no longer says `read-only`. The model name stays on the right. `pgdn to follow`, the spinner, and `copied` are unchanged.

## Release

A tag matching `v*` runs `.github/workflows/release.yml`. The test job runs `go test ./...`. The release job then builds four binaries with `CGO_ENABLED=0`: `darwin/arm64`, `darwin/amd64`, `linux/amd64`, and `linux/arm64`. The version ldflag is the tag without the leading `v`. The commit is the short HEAD and the date is UTC. The files are `dist/co_<version>_<goos>_<goarch>`. `gh release create` publishes them with generated notes. A tag matching `v<major>.<minor>.<patch>-` is marked a prerelease. `v0.1.0-rc.1` points at the commit that added this workflow.

Push and pull request still run test and lint. The test workflow compiles `./cmd/co` before `go test ./...`. Lint is still `golangci-lint` v2.15.0.

## Build

`make build`, `make test`, and `make lint` are unchanged. The built binary is gitignored.

`docs/architecture.excalidraw` now says the tool loop can create and edit, and it lists `create_file` and `edit_file` on `tools.Set`. Language servers, web tools, and skills are not on that drawing. `docs/lsp.md` says the writes land inside the jail and that the server is still not asked to apply an edit.

## Tests

`go test ./...` covers creating a file, refusing a second create, the 1 MiB cap, a single-snippet edit, a symlink that stays inside the directory, a symlink that leaves it, skill loading and the project override, a skill that applies to one turn and not the next, `/skill` in both fronts, the grouped slash list, the skill picker inserting a name, resume keeping skill names without putting the body back into the prompt, and the release-less prompt rules for `create_file` and `edit_file`.
