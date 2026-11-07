//go:build darwin

package tools

import (
	"fmt"
	"os"
)

const sandboxExec = "/usr/bin/sandbox-exec"

func bashCommand(spec bashSpec) (string, []string, error) {
	if _, err := os.Stat(sandboxExec); err != nil {
		return "", []string{}, fmt.Errorf("sandbox-exec is not available")
	}
	if _, err := os.Stat(bashBinary); err != nil {
		return "", []string{}, fmt.Errorf("bash is not available")
	}
	argv := []string{"-p", seatbeltProfile(), bashBinary, "-c", spec.command}
	return sandboxExec, argv, nil
}

// seatbeltProfile allows every operation. sandbox-exec requires a profile,
// and (allow default) is that profile's default action.
func seatbeltProfile() string {
	return "(version 1)\n(allow default)\n"
}
