---
title: Settings
description: The settings file, which file wins, and the flags co reads.
---

The first run creates `~/.co/config.yaml` and `~/.co/models.yaml` when either file is missing. A later run leaves an existing file as it is. `--config` does not create the home files.

`~/.co/config.yaml` is the settings file. It holds the default model and the flags that are safe to keep. The generated file has the flags below and no `model` key. Choosing a model adds that key:

```yaml
model: openrouter/qwen/qwen3-coder
theme: teal
log-level: info
log-retention-days: 3
no-history: false
clear-scrollback: false
max-tool-rounds: 64
```

`theme` is the TUI palette: `teal`, `amber`, `rose`, `sky`, `violet`, or `ink`. The default is `teal`. Each palette has a light side and a dark side, and the TUI picks the side from the terminal background. `log-level` is `debug`, `info`, `warn`, or `error`. Log lines go to `~/.co/logs/co-YYYY-MM-DD.log`, using the local date. `log-retention-days` is how many of those daily files to keep, including today. The default is 3. There is no flag for it. A file from an older day is removed when `co` starts. After midnight, the next log line opens that day's file and removes files outside the window. `no-history` skips the transcript. `clear-scrollback` erases the terminal scrollback before the TUI starts. `max-tool-rounds` is how many model calls in a turn may include tools. After that many, one more call is made with no tools. The default is 64. A flag or its environment variable wins over the file. `resume` is not read from the file.

## Which file is used

| Source | Role |
| --- | --- |
| `--model`, or `CO_MODEL` | Model for this process. Does not change the config file. |
| `model` in `~/.co/config.yaml` | Default model. |
| `model` in `./.co.yaml` | Default model when the home file has none. |
| `providers` in `~/.co/models.yaml` | Home providers. |
| `providers` in `./.co.yaml` | Replaces the home provider with the same id. Other home providers stay. |
| `theme`, `log-level`, `log-retention-days`, `no-history`, `clear-scrollback`, `max-tool-rounds` in `~/.co/config.yaml` | Defaults for those settings. |

`--config path` uses that file alone for flags, providers, and the default model. It does not read `~/.co/config.yaml`, `~/.co/models.yaml`, or `./.co.yaml`, and `/models` does not write the home file. Keys still come from the environment and `~/.co/credentials.json`.

`./.co.yaml` is per project. Leave it out of git if it names private endpoints.

## Flags

| Flag | Environment | `config.yaml` | |
| --- | --- | --- | --- |
| `--config` | | | Replaces the home and project files. |
| `--log-level` | `CO_LOG_LEVEL` | `log-level` | `debug`, `info`, `warn`, or `error`. Default `info`. Logs go to `~/.co/logs`. |
| | `CO_LOG_RETENTION_DAYS` | `log-retention-days` | Days of log files to keep. Default 3. |
| `--model` | `CO_MODEL` | `model` | `provider/model-id` for this process. The file value is the default model. |
| `--resume` | `CO_RESUME` | | A session id, or `latest` for the newest session in this directory. Not read from the file. |
| `--no-history` | `CO_NO_HISTORY` | `no-history` | Do not write the transcript. |
| `--clear-scrollback` | `CO_CLEAR_SCROLLBACK` | `clear-scrollback` | Erase the terminal scrollback before the TUI starts. Off unless set. |
| `--max-tool-rounds` | `CO_MAX_TOOL_ROUNDS` | `max-tool-rounds` | Model calls that may include tools. Default 64. The next call has no tools. |
| `--theme` | `CO_THEME` | `theme` | TUI palette: `teal`, `amber`, `rose`, `sky`, `violet`, or `ink`. Default `teal`. |

`CO_` is the environment prefix, and `-` in a flag name becomes `_`.
