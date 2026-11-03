package lsp

// serverSpec is one language server.
// Command is the process key, so extensions that share a binary share a process.
// Files maps a file extension, including the dot, to an LSP language id.
// Init is sent as initializationOptions. gopls needs none.
// Adding a language is another value here, not a new client.
type serverSpec struct {
	Command string
	Args    []string
	Files   map[string]string
	Init    any
}

func builtinServers() []serverSpec {
	return []serverSpec{
		{
			Command: "gopls",
			Args:    []string{},
			Files:   map[string]string{".go": "go"},
		},
	}
}
