# 05 — Bash and the handbook

Baseline commit: `33963cf1032f5548444efdf4b4b4761a7374b73e` (`33963cf`)

| | |
| --- | --- |
| Subject | chore: correct logo size in readme |
| Author | kevinjom |
| Date | 2026-11-13 21:08:55 +0800 |
| Branch | `main` |

This file describes what changed after [04](04-writes-and-skills.md). File tools are still jailed to the working directory. The next iteration starts here:

```
git log --oneline 33963cf1032f5548444efdf4b4b4761a7374b73e..HEAD
git diff 33963cf1032f5548444efdf4b4b4761a7374b73e..HEAD
```

## What co is now

The model can run a shell command. That command can read the rest of the disk, use the network, and write anywhere the user can. Creating and editing source files stays on `create_file` and `edit_file`.

File search prefers `rg`, then `ag`. The `grep` tool is registered only when both are missing. A skill named in a user message is stored with that message and sent again on later turns, including after resume. The TUI shows each tool result and how long the turn took.

A missing `~/.co/config.yaml` and `~/.co/models.yaml` are created on startup. `install.sh` installs a release binary. The handbook is published from stable tags at [https://kevinjom.github.io/co](https://kevinjom.github.io/co).

`internal/ui` is now `internal/tui`. Chat types and the streaming port are `internal/llm`. The OpenAI client is `internal/llm/openai`.

## Bash

`bash` is always registered. The only argument is `command`. There is no allowlist and no working-directory argument. The command starts in the directory `co` was started in, as soon as the model calls the tool.

The prompt tells the model to run commands with `bash`, that `bash` can read outside the working directory and use the network, and that it can write anywhere. It also tells the model to leave creating and editing source files to `create_file` and `edit_file`.

| | |
| --- | --- |
| Read | The rest of the disk, including paths outside the working directory. |
| Network | Outbound connections are allowed. |
| Write | Anywhere the user can write. |
| Stdin | Not a terminal. The command takes no input. |
| Timeout | 2 minutes. The model cannot raise it. Canceling the turn kills the command and its process group. A process that leaves the group can outlive the call. |
| Output | Stdout and stderr are one stream. Longer than 32 KiB keeps the last 32 KiB and says `truncated: true`. |

A non-zero exit is a normal result, so a failing test is data for the model. Failure to start, a missing `bash`, a timeout, and a cancel are errors.

On macOS the command runs under `sandbox-exec` with a profile that allows every operation. If that profile cannot be applied because the process is already sandboxed, the command runs directly. A missing `sandbox-exec` fails the call. On Linux the command is not sandboxed. On any other system, `bash` returns an error and does not run.

The command inherits the environment of `co`. `TMPDIR`, `GOCACHE`, and `GOTMPDIR` are set to `~/.co/bash`, created at mode `0700` when a command runs. `~/.co` is set to `0700` at the same time. `GOMODCACHE` is left as it is.

The result is the exit code, then the output:

```
exit: 0
hello
```

The TUI labels the row `Bash` and shows the command under it, wrapped to the width of the transcript. The row keeps the command after the call finishes. While the call is running, the status line is `Bash` plus the command.

## Search

When a session's tools are built, `co` looks up `rg`, then `ag`, on `PATH`. A binary installed after that session starts is not picked up by it.

| Found | What the model is told | Tool |
| --- | --- | --- |
| `rg` | Search file contents with `rg`. Prefer the language-server tools over `rg` for definitions, references, and diagnostics. | `grep` is omitted. The model reaches `rg` through `bash`. |
| `ag`, and no `rg` | The same, naming `ag`. | `grep` is omitted. |
| neither | Search file contents with the `grep` tool. | `grep` is registered, as before: a Go regular expression, `.git` skipped, at most 50 matches. |

## Tool rows

`ToolDone` carries the text the model received. The TUI keeps that text under the call summary once the call finishes. A failed call is drawn in the error color. The result is clipped to 8 lines, with `…` when there is more. An empty result draws nothing under the summary.

The line reader prints `tool <name> <summary>` on stderr when a call starts. When the call fails it prints `tool <id> error: …`. A successful result stays in the conversation and is not printed there.

A finished TUI turn adds a note under the answer. Under a second it is `cooked for 12ms`. Up to a minute it rounds to a tenth of a second, or to whole seconds when that tenth is zero. A longer turn rounds to a second, and an exact number of minutes is `cooked for 2m`. Anything under a millisecond is shown as `1ms`.

## Skills on the message

The system prompt still lists each loaded skill as `name: description` and says a `/name` applies for that turn only. The skill body is not copied into the system prompt.

The body is attached to the user message that named the skill. The line stored in the session is the line as typed. Each later request copies that message and appends the same skill block, so a later turn sends the body again. A message that names no skill sends no block.

The transcript records the attachment beside that message. `at` is the sequence number of the message. `body` is the `SKILL.md` text from that turn. `dir` is `.agents/skills/<name>` for a project skill and omitted for a home skill.

```json
{"type":"skill","v":1,"seq":4,"ts":"RFC3339Nano","at":2,"name":"grilling","dir":".agents/skills/grilling","body":"---\nname: grilling\n..."}
```

Resume binds that stored body back onto the message. It sends the text from the turn that named the skill, so a skill edited afterward stays as it was for that session. A name-only record, the shape written in [04](04-writes-and-skills.md), is kept as a name and is not attached to a message. A record with `at` and an empty body is an error. An empty name is still an error.

## First launch

When `--config` is unset, a missing `~/.co/config.yaml` is created mode `0644`. An existing file, including an empty one, is left as it is. The directory is mode `0700`. The new file is the defaults:

```yaml
theme: teal
log-level: info
log-retention-days: 3
no-history: false
clear-scrollback: false
max-tool-rounds: 64
```

A missing `~/.co/models.yaml` is created the same way, with Gemini and OpenRouter:

```yaml
providers:
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GEMINI_API_KEY
    models:
      - gemini-flash-latest
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
```

`co ollama` still adds its provider beside those blocks.

Until a session exists, the model picker is the screen. `/` opens the composer so `/connect` can run before a model is chosen. Esc on the model list quits. Esc from `/connect`, the provider list, or the command list returns to the model list. After a key is saved, the picker is shown again, including the `saved key for <id>` note. Once a session exists, `/` typed into the model list is a filter.

## Install and the handbook

`install.sh` installs a release binary for darwin or linux, amd64 or arm64, into `PREFIX` or `/usr/local/bin`. With no version it installs the latest stable release. A prerelease is not that release: if the newest published release is a prerelease, the script names it and stops. A version may be passed as an argument or as `CO_VERSION`, with or without the leading `v`. The download is checked for an ELF or Mach-O header. A prefix that is not writable is installed with `sudo`.

```sh
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/kevinjom/co/main/install.sh | sh -s -- 0.1.0
```

The handbook lives in `site/` and is served at `https://kevinjom.github.io/co`. A pull request builds it and does not deploy. A stable tag `v<major>.<minor>.<patch>` publishes the three newest stable tags that contain `site/`. The newest is written at the site root and under `/<tag>/`. Each older tag is written only under its own prefix. A prerelease tag does not deploy. The README points at that site and says it follows the latest stable release.
