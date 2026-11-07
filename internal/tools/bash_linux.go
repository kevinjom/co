//go:build linux

package tools

import (
	"fmt"
	"os"
)

// bashCommand starts bash with no sandbox.
// Revisit later: confine writes with Landlock from a child process.
// Applying Landlock in this process would confine co itself.
func bashCommand(spec bashSpec) (string, []string, error) {
	if _, err := os.Stat(bashBinary); err != nil {
		return "", []string{}, fmt.Errorf("bash is not available")
	}
	return bashBinary, []string{"-c", spec.command}, nil
}
