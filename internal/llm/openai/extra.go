package openai

import "github.com/kevinjom/co/internal/llm"

func thoughtSignature(extra *llm.ExtraContent) string {
	if extra == nil || extra.Google == nil {
		return ""
	}
	return extra.Google.ThoughtSignature
}

func withThoughtSignature(signature string) *llm.ExtraContent {
	if signature == "" {
		return nil
	}
	return &llm.ExtraContent{Google: &llm.GoogleExtra{ThoughtSignature: signature}}
}
