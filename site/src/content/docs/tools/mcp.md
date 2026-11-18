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

Header values are sent as written. `co` does not read them from the environment. A literal `Authorization` header is the credential, and a `401` is then a tool error.

Without that header, a `401` starts an OAuth 2.1 authorization-code login with PKCE. A `403` that asks for a broader scope starts the same login. `co` opens a browser and waits on `http://127.0.0.1:8737/callback` for up to 5 minutes. Each HTTP request still times out at 2 minutes. Refresh tokens are stored in `~/.co/mcp-tokens.json`, mode `0600`, keyed by the server name. A later call refreshes without a browser. The file is not created until a login succeeds.

`client_id` and `client_secret` are optional. Leave both out and `co` registers a native public client. That succeeds only when the authorization server allows dynamic registration. GitHub does not. Its token endpoint requires the OAuth app's `client_id` and `client_secret`, and PKCE does not replace the secret. The app's callback URL must be `http://127.0.0.1:8737/callback`. A `client_secret` without a `client_id` stops startup.

```yaml
github:
  url: https://api.githubcopilot.com/mcp/
  client_id: your-oauth-app-id
  client_secret: your-oauth-app-secret
```

The model does not receive the remote catalog. A server may advertise hundreds of tools. Those schemas stay on the client. The model gets two tools:

| Tool | Arguments |
| --- | --- |
| `mcp_search` | `query`. Words matched against tool names and descriptions. |
| `mcp_call` | `server`, `tool`, and `arguments` from a search hit. |

`mcp_search` returns at most 8 hits. Each hit has the server name, the tool name, the description, and the input schema. Hits follow the order of servers in the file, then the tool name. A server that fails the version check, the connection, or `tools/list` is left out of the hits and named under `unavailable`. An empty query is an error.

`mcp_call` runs as soon as the model calls it. Text parts are joined. `structuredContent` is appended as JSON. A non-text part is counted, for example `omitted: 2 images`. A result that asks for more input is an error. Output longer than 32 KiB keeps the last 32 KiB and says `truncated: true`. An HTTP request longer than 2 minutes is an error. Canceling the turn aborts the request and any login still in progress.

`tools/list` is read in full, following `nextCursor`, and kept until the server's `ttlMs` elapses. The next search fetches it again after that. One hub is shared by every session and closed when `co` exits.
