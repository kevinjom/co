---
title: Tools
description: The tools the model can call, and the directory jail around file tools.
---

The model gets filesystem tools, jailed to the working directory, a shell command, web search and page fetch, and language-server tools when a server binary is on `PATH`. Today that is `gopls` for `.go` files. See [Language server](../../tools/lsp/) and [Bash](../../tools/bash/).

| Tool | |
| --- | --- |
| `list_dir` | One directory level. Path defaults to `.`. |
| `read_file` | A window of a text file. Defaults to 200 lines from line 1. |
| `create_file` | Create a new file. Fails if the path already exists. |
| `edit_file` | Replace one snippet that appears once. Other bytes stay as they are. |
| `bash` | Run a shell command. See [Bash](../../tools/bash/). |
| `grep` | A Go regular expression. Skips `.git`. Returns at most 50 matches. |
| `web_search` | DuckDuckGo search. Defaults to 8 results, at most 10. |
| `web_fetch` | One http or https page, as Markdown. |
| `lsp_definition` | Definition of the symbol at a line and column. |
| `lsp_references` | References to that symbol, including the declaration. |
| `lsp_hover` | Hover text from the language server. |
| `lsp_symbols` | Symbols in one file (`path`) or a workspace search (`query`). |
| `lsp_diagnostics` | Diagnostics for one file, or the workspace when `path` is omitted. |

Paths are resolved, and a path that leaves the directory is rejected. `create_file` only creates a missing file. `edit_file` replaces one snippet that appears once. Line and column start at 1. A language-server result may name a file outside the working directory, and it does not include that file's body. Rename, formatting, and code actions are not tools.
