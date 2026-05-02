//go:build !windows

package profile

import (
	"os/exec"
	"syscall"
)

// SetProcessGroup arranges for the spawned process to live in its own
// process group so KillProcessGroup can take down the whole tree at once.
// Chrome forks renderer/GPU/zygote children that do NOT die when the parent
// receives SIGKILL — without process-group teardown they survive briefly,
// holding file handles in the user-data-dir and breaking tempdir cleanup.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroup sends SIGKILL to every process whose pgid is `pid`
// (the negative-pid trick). Returns nil if the group is already gone.
func KillProcessGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	return nil
}

// internal aliases for backward compat within the package.
func setProcessGroup(cmd *exec.Cmd) { SetProcessGroup(cmd) }
func killProcessGroup(pid int) error { return KillProcessGroup(pid) }
