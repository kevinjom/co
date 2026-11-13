// Package mcp is an HTTP client for Model Context Protocol servers.
// Servers are listed in ~/.co/mcp.yaml. The model does not receive their
// tool schemas. It searches a local catalog and calls one tool at a time.
//
// The client speaks protocol 2026-07-28 on Streamable HTTP. A server that
// does not complete the session on that version is skipped.
package mcp
