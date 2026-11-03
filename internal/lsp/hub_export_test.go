package lsp_test

import (
	"github.com/kevinjom/co/internal/lsp"
	"github.com/kevinjom/co/internal/tools"
)

// Hub is the language service the tools call.
var _ tools.Language = (*lsp.Hub)(nil)
