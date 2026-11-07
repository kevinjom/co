//go:build linux

package tools

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != sandboxArg {
		return
	}
	if err := execSandboxed(os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: %s\n", err)
		os.Exit(1)
	}
}

func execSandboxed(args []string) error {
	if len(args) != 4 || args[2] != "--" || args[3] == "" {
		return fmt.Errorf("bad sandbox arguments")
	}
	if err := restrictWrites([]string{args[0], args[1]}); err != nil {
		return err
	}
	return unix.Exec(bashBinary, []string{"bash", "-c", args[3]}, os.Environ())
}

func bashCommand(spec bashSpec) (string, []string, error) {
	abi, err := landlockABI()
	if err != nil || abi < 3 {
		return "", []string{}, fmt.Errorf("landlock is not available")
	}
	if _, err := os.Stat(bashBinary); err != nil {
		return "", []string{}, fmt.Errorf("bash is not available")
	}
	exe, err := os.Executable()
	if err != nil {
		return "", []string{}, fmt.Errorf("finding executable: %w", err)
	}
	argv := []string{sandboxArg, spec.writes[0], spec.writes[1], "--", spec.command}
	return exe, argv, nil
}

func landlockABI() (int, error) {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 1 {
		return 0, fmt.Errorf("landlock is not available")
	}
	return int(abi), nil
}

// restrictWrites denies creating, changing, and removing files except under paths.
// Reads and the network are not in the ruleset, so they stay allowed.
// Truncate is only enforceable on Landlock ABI 3, which is Linux 6.2.
func restrictWrites(paths []string) error {
	abi, err := landlockABI()
	if err != nil || abi < 3 {
		return fmt.Errorf("landlock is not available")
	}
	access := writeAccess(abi)
	attr := unix.LandlockRulesetAttr{Handled_access_fs: access}
	fd, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)),
		8,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	defer unix.Close(int(fd))

	for _, path := range paths {
		if err := allowPath(int(fd), path, access); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("landlock: %w", err)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	return nil
}

func allowPath(ruleset int, path string, access uint64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("landlock: %w", err)
	}
	defer f.Close()
	attr := unix.LandlockPathBeneathAttr{
		Allowed_access: access,
		Parent_fd:      int32(f.Fd()),
	}
	_, _, errno := unix.Syscall6(
		unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(ruleset),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&attr)),
		0,
		0,
		0,
	)
	if errno != 0 {
		return fmt.Errorf("landlock: %w", errno)
	}
	return nil
}

func writeAccess(abi int) uint64 {
	access := uint64(
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
			unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
			unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
			unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG |
			unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
			unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
			unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
			unix.LANDLOCK_ACCESS_FS_MAKE_SYM,
	)
	if abi >= 2 {
		access |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		access |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	return access
}
