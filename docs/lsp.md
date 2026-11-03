# Language servers

`co` speaks the Language Server Protocol as a client. The model calls it the same way it calls `grep`. The assistant stays read-only: the server is asked for definitions, references, hover, symbols, and diagnostics. It is not asked to rename, format, or apply a code action.

The code lives in `internal/lsp`. `internal/tools` turns that client into tools. `internal/command.App` owns one hub and shares it with every session. `App.Close` shuts the servers down on the way out, which `cmd/co` already calls.

## Tools

The tools are registered only when at least one row's binary is on `PATH`. If none is, `co` logs `language tools disabled` and the model keeps using `grep`. A missing binary hides that row only. CI does not install `gopls`.

Line and column start at 1. The column counts Unicode code points. Results are capped at 50 entries, and a capped result says `truncated: true`, same as `grep`.

| Tool | Method | Arguments |
| --- | --- | --- |
| `lsp_definition` | `textDocument/definition` | `path`, `line`, `column` |
| `lsp_references` | `textDocument/references` | `path`, `line`, `column`. The declaration is included. |
| `lsp_hover` | `textDocument/hover` | `path`, `line`, `column` |
| `lsp_symbols` | `textDocument/documentSymbol` or `workspace/symbol` | `path` for one file, or `query` for the workspace. One of the two, not both. |
| `lsp_diagnostics` | `textDocument/diagnostic` or `workspace/diagnostic` | `path` for one file. Omit it for the workspace. |

A server that did not advertise pull diagnostics makes `lsp_diagnostics` return `language server has no pull diagnostics`. `co` does not collect `textDocument/publishDiagnostics`. An unchanged pull report returns `diagnostics unchanged`.

A file result looks like this:

```
locations: 1
main.go:10:4
```

```
symbols: 1
function Example main.go:1:1
```

```
diagnostics: 1
main.go:3:1 error undefined: x
```

Hover is the server's text, or `hover: empty`. Hover text longer than 8192 code points is cut and marked `truncated: true`.

`lsp_symbols` with a query, and `lsp_diagnostics` with no path, have no file. Those call every row whose binary is on `PATH` and merge the lines, still capped at 50. Each of those lines starts with the binary's name, as in `gopls function Example main.go:1:1`. A file tool does not add that prefix.

When the tools are on, the system prompt tells the model to prefer them over `grep` for definitions, references, and diagnostics. The transcript line for a position tool is `path:line:column`.

## Servers

The five tools do not name a language. A table routes by file extension. Each row is one process, keyed by its command, so extensions that share a binary share a process. The process starts on the first call that needs it, not at startup. Its working directory and workspace folder are the directory `co` was started in.

| Field | |
| --- | --- |
| `command`, `args` | The binary and the arguments that make it speak LSP on stdin and stdout. |
| `files` | File extension, including the dot, to the LSP `languageId` sent on `didOpen`. |
| `init` | Optional `initializationOptions`. Empty when the server needs none. |

The only row shipped now:

| Command | Args | Files | Init |
| --- | --- | --- | --- |
| `gopls` | none | `.go` → `go` | none |

`gopls` with no arguments is its stdio server. A call on any other extension returns `no language server for` that extension, and does not start `gopls`.

Adding a language is another row. A TypeScript row would be `typescript-language-server` with `--stdio`, `.ts` mapped to `typescript`, `.tsx` mapped to `typescriptreact`, and whatever `initializationOptions` that server requires. The tools, the jail, the position conversion, and the document sync stay as they are. Each client syncs only the files it was asked about.

Initialize sends `co`'s pid, `rootUri`, and one workspace folder. The client asks for position encodings `utf-8` then `utf-16`, and uses whichever the server picks. If the server omits it, the offset is UTF-16, which is the protocol default. The client replies to `workspace/configuration` with nulls and to `workspace/workspaceFolders` with that folder, and it answers other server requests with method-not-found. It does not advertise `workspace.applyEdit`.

Canceling a turn sends `$/cancelRequest` for the call in flight. The server process keeps running. The next tool call can use it.

Server stderr is logged at debug. It is not part of the transcript.

## Files the server is shown

Before a request about a file, `co` reads that file from disk.

- The first time, it sends `textDocument/didOpen` with the whole file and the row's language id.
- If the bytes on disk differ from what the server already has, it sends one `textDocument/didChange` whose change has no range. A change without a range replaces the whole file.

There is no incremental sync, so a later writer does not have to compute a diff. It saves the file inside the jail, and the next request takes the same path. That writer does not exist yet. The capability set is one function, `readOnlyCapabilities`, and a write mode is that function advertising `workspace.applyEdit` plus a handler that applies a `WorkspaceEdit` through the existing path jail.

## Paths

A path the model passes in goes through the same jail as `read_file`. A symlink that leaves the working directory is rejected.

The server process is not jailed. `gopls` may read the module cache. Tool results must not become a way to read those files.

- A location inside the working directory is a relative path, a 1-based line, and a 1-based code-point column.
- A location outside it is an absolute path, the server's line and character, and the words `outside the working directory`. The file is not opened, and its body is not returned.
- Hover text is returned as the server sent it. That text is the signature and the docs, not a file read.

## What this does not do

Rename, formatting, code actions, and applying edits are not tools. `co` does not write files, and it does not tell the server that it can apply a workspace edit.
