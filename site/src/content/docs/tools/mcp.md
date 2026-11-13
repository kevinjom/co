---
title: MCP
description: How co calls HTTP MCP servers without putting their catalogs on the model request.
---

`co` is an MCP client. It speaks protocol `2026-07-28` over Streamable HTTP. A server that does not list that version is skipped. Older revisions are not used. `co` does not expose itself as an MCP server.

Servers are read from `~/.co/mcp.yaml`. A missing file is left missing, and the MCP tools are not registered. `--config` does not change this path. Broken YAML, a server without `url`, or a name outside letters, digits, `_`, and `-` stops startup.

```yaml
github:
  url: https://example.com/mcp
  headers:
    Authorization: Bearer hardcoded-token
```

Header values are sent as written. `co` does not read them from the environment. A `401` or `403` is a tool error.

The model does not receive the remote catalog. A server may advertise hundreds of tools. Those schemas stay on the client. The model gets two tools:

| Tool | Arguments |
| --- | --- |
| `mcp_search` | `query`. Words matched against tool names and descriptions. |
| `mcp_call` | `server`, `tool`, and `arguments` from a search hit. |

`mcp_search` returns at most 8 hits. Each hit has the server name, the tool name, the description, and the input schema. Hits follow the order of servers in the file, then the tool name. A server that fails the version check, the connection, or `tools/list` is left out of the hits and named under `unavailable`. An empty query is an error.

`mcp_call` runs as soon as the model calls it. Text parts are joined. `structuredContent` is appended as JSON. A non-text part is counted, for example `omitted: 2 images`. A result that asks for more input is an error. Output longer than 32 KiB keeps the last 32 KiB and says `truncated: true`. A request longer than 2 minutes is an error. Canceling the turn aborts the request.

`tools/list` is read in full, following `nextCursor`, and kept until the server's `ttlMs` elapses. The next search fetches it again after that. One hub is shared by every session and closed when `co` exits.
