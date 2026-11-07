//go:build !darwin && !linux

package tools

import (
	"fmt"
	"os/exec"
)

func bashCommand(bashSpec) (string, []string, error) {
	return "", []string{}, fmt.Errorf("bash is not available on this operating system")
}

func configureCmd(*exec.Cmd) {}
