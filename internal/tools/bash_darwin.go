//go:build darwin

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const sandboxExec = "/usr/bin/sandbox-exec"

func bashCommand(spec bashSpec) (string, []string, error) {
	if _, err := os.Stat(sandboxExec); err != nil {
		return "", []string{}, fmt.Errorf("sandbox-exec is not available")
	}
	if _, err := os.Stat(bashBinary); err != nil {
		return "", []string{}, fmt.Errorf("bash is not available")
	}
	if !sandboxApplies() {
		return bashBinary, []string{"-c", spec.command}, nil
	}
	argv := []string{"-p", seatbeltProfile(), bashBinary, "-c", spec.command}
	return sandboxExec, argv, nil
}

// sandboxApplies reports whether sandbox-exec can enter a profile.
// A process that is already sandboxed is denied. The profile allows every
// operation, so the command runs directly in that case.
func sandboxApplies() bool {
	sandboxOnce.Do(func() {
		cmd := exec.Command(sandboxExec, "-p", seatbeltProfile(), bashBinary, "-c", "exit 0")
		out, err := cmd.CombinedOutput()
		if err == nil {
			sandboxOK = true
			return
		}
		sandboxOK = !strings.Contains(string(out), "Operation not permitted")
	})
	return sandboxOK
}

// seatbeltProfile allows every operation. sandbox-exec requires a profile,
// and (allow default) is that profile's default action.
func seatbeltProfile() string {
	return "(version 1)\n(allow default)\n"
}

var (
	sandboxOnce sync.Once
	sandboxOK   bool
)
