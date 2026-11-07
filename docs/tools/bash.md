# Bash

`co` runs one shell command when the model calls `bash`. The argument is `command`, a string. There is no allowlist and no working-directory argument. The command starts as soon as the model calls the tool. The code lives in `internal/tools`.

Creating and editing files stays on `create_file` and `edit_file`.

## What the command can touch

The process working directory is the directory `co` was started in.

The command can read the rest of the disk, including files outside the working directory. It can open outbound network connections.

On macOS the command runs under `sandbox-exec` with a profile that allows every operation. It can write anywhere the user can.

On Linux the command runs under Landlock. It can create, overwrite, truncate, rename, and delete files only in the working directory and in `~/.co`. That includes credentials, config, logs, transcripts, and skills under `~/.co`. A symlink that points outside those two directories does not open a hole.

On any other system, `bash` returns an error and does not run the command. If the sandbox is missing, `co` does not fall back to an unsandboxed run.

## Environment

The command inherits the environment of `co`. `TMPDIR`, `GOCACHE`, and `GOTMPDIR` are set to `~/.co/bash`, which `co` creates at mode `0700` when a command runs. `GOMODCACHE` is left as it is. On Linux a fetch that needs to write the module cache fails. On macOS that write is allowed.

## Lifetime

Stdin is not a terminal. The command does not take input. Stdout and stderr come back as one stream.

A command still running after 2 minutes is killed. The model cannot raise that limit. Canceling the turn kills the command and the processes in its group. A process that leaves the group can outlive the call.

A non-zero exit is a normal result, so a failing test is data for the model. Failure to start, a missing sandbox, a timeout, and a cancel are errors.

## Result

The result is the exit code, then the output. Output longer than 32 KiB keeps the last 32 KiB and says `truncated: true` before that output.

```
exit: 0
hello
```

```
exit: 1
truncated: true
the last 32 KiB
```

## The transcript

The TUI labels the row `Bash` and shows the command under it, wrapped to the width of the transcript. The row keeps the command after the call finishes. While the call is running, the status line is `Bash` plus the command.
