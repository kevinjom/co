---
title: Sessions and commands
description: How to start co, the slash commands, and where transcripts are stored.
---

```sh
co
co --model ollama/qwen3:8b
co --resume latest
co --resume 20261102T120000Z-ab12 --no-history
co session list
```

With no model selected, a terminal opens the model picker. `/` lists commands there, so `/connect` can save a key before a model is chosen. Esc from that picker quits. Anything else exits and asks for `--model` or `model` in `~/.co/config.yaml`.

Flags, environment variables, and `config.yaml` are on [Settings](../../setup/settings/).

## In the session

| Line | |
| --- | --- |
| `/models` | List configured models. In the TUI this opens a picker. |
| `/models provider/model-id` | Switch the model for the session on screen. The choice is saved to `~/.co/config.yaml`, unless `--config` was set. |
| `/connect` | List providers that accept an API key. |
| `/connect provider` | Save an API key for that provider. |
| `/new` | Start another session in the TUI and show it. Other sessions keep running. |
| `/sessions` | Switch the session on screen. In the TUI this opens a picker. |
| `/theme` | List palettes. In the TUI this opens a picker. |
| `/theme name` | Switch the palette. The choice is saved to `~/.co/config.yaml`, unless `--config` was set. |
| `/skill` | List skills. In the TUI this opens a picker. Choosing one inserts `/name` and leaves the cursor after it. |
| `/name` | Use that skill for this turn. Type the rest of the message, then send. |
| `/stats` | Show counts for this session: user turns, assistant answers, tool calls, and provider-reported tokens. |
| `exit`, `quit` | Leave. When a transcript was written, `co` prints the id to resume. |

A skill is a directory with a `SKILL.md` file, as in the [Agent Skills specification](https://agentskills.io/specification). `co` reads `<working directory>/.agents/skills/<name>/SKILL.md` and `~/.co/skills/<name>/SKILL.md` at startup. The same name in the project wins. The system prompt lists each skill's name and description. A message that names `/name` follows that skill for that turn only. `co` does not run scripts from a skill.

`/new` and `/sessions` are TUI commands. The line reader keeps a single session.

`/stats` counts that session, including messages loaded with `--resume`. A user message is a user turn. An assistant message that has text is an assistant answer. A tool result is a tool call. Input and output tokens are the sums the provider reported for each model request. Context is the input size of the latest of those requests. The request that names the session is not included. When no request reported usage, the token lines say `unknown`. When some did and some did not, the total is marked `incomplete`. Switching models keeps one set of totals. The TUI writes the block into the transcript. The line reader prints it on stderr.

In the TUI, Tab completes slash commands. Typing `/` lists them in two groups, `commands` and `skills`. The command group is `/models`, `/connect`, `/new`, `/sessions`, `/stats`, `/theme`, and `/skill`. The skills group is one `/name` for each skill. Up and down move through the names, and the headings stay put. Enter runs a command. Tab or Enter on a skill inserts `/name` and leaves the cursor after it, the same way `@` inserts a file, so the rest of the turn can be typed before it is sent. Ctrl+J inserts a newline, and Ctrl+C quits. Typing `@` opens a fuzzy file list for this directory. The text after `@` is the query. Up and down move, Tab or Enter inserts `@path`, and Esc closes the list. The list skips `.git` and paths excluded by `.gitignore`. The transcript fills the terminal, with a session list on the right when the terminal is at least 60 columns wide. Each row is a status icon and the session title. The icon animates while that session is working. The list is only the sessions opened in this TUI. The mouse wheel, trackpad, PgUp, and PgDn scroll the transcript, and it stays pinned to the bottom until you scroll up. Ctrl+C during a turn stops that turn. Other sessions keep going. Ctrl+C while the session on screen is idle quits.

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
