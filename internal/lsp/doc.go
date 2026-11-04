// Package lsp is a read-only Language Server Protocol client.
// One hub is shared by every session. Each server row is its own process,
// started on the first request that routes to it.
//
// The server process is not jailed and may read a module cache. Results never
// include the body of a file outside the working directory.
//
// Document sync sends the full file text. The first use of a file is didOpen.
// A later read sends didChange when the bytes on disk differ, which is the
// path a future writer will take after it saves a file.
// Client capabilities do not advertise workspace.applyEdit.
package lsp
