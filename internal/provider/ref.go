package provider

import (
	"fmt"
	"strings"
)

// Ref is a selected model. Provider is the text before the first slash.
// Model is the rest, and it is the id sent to the provider.
type Ref struct {
	Provider string
	Model    string
}

func (r Ref) String() string {
	return r.Provider + "/" + r.Model
}

// ParseError means the value is not provider/model-id.
type ParseError struct {
	Raw string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("model %q must be provider/model-id", e.Raw)
}

// Parse splits a selection on the first slash.
// The model id may contain further slashes. A value with no slash is rejected.
func Parse(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	provider, model, ok := strings.Cut(raw, "/")
	if !ok || provider == "" || model == "" || strings.ContainsAny(provider, " \t") {
		return Ref{}, &ParseError{Raw: raw}
	}
	return Ref{Provider: provider, Model: model}, nil
}
