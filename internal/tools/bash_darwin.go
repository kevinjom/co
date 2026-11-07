//go:build darwin

package tools

import (
	"fmt"
	"os"
	"strings"
)

const sandboxExec = "/usr/bin/sandbox-exec"

func bashCommand(spec bashSpec) (string, []string, error) {
	if _, err := os.Stat(sandboxExec); err != nil {
		return "", []string{}, fmt.Errorf("sandbox-exec is not available")
	}
	if _, err := os.Stat(bashBinary); err != nil {
		return "", []string{}, fmt.Errorf("bash is not available")
	}
	argv := []string{"-p", seatbeltProfile(spec.writes), bashBinary, "-c", spec.command}
	return sandboxExec, argv, nil
}

// seatbeltProfile allows every operation, then denies file writes, then
// allows writes under the two real directories. The last matching rule wins.
func seatbeltProfile(writes []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	for _, path := range writes {
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", path)
	}
	return b.String()
}
