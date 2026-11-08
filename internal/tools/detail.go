package tools

import "strings"

var details = map[string]func(map[string]any) string{}

func registerDetail(name string, fn func(map[string]any) string) {
	details[name] = fn
}

// Detail is the short argument summary shown for a tool call.
// A tool with no summary of its own is shown as path= plus the path.
func Detail(name string, args map[string]any) string {
	if args == nil {
		args = map[string]any{}
	}
	if fn, ok := details[name]; ok {
		return fn(args)
	}
	return "path=" + detailPath(args)
}

func detailPath(args map[string]any) string {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}

func detailInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
