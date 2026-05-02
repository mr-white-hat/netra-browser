//go:build windows

package profile

import (
	"os"
	"os/exec"
)

// On Windows, child processes don't inherit a kill signal from a
// process-group concept the way they do on Unix. The Job Objects API
// is the right primitive but adds enough complexity that v1 ships
// the simpler "kill parent and trust the OS to reap children" path.
// If Chrome zombies become a documented Windows pain point, switch
// to JobObjects via golang.org/x/sys/windows.
// SetProcessGroup is a no-op on Windows. The Job Objects API is the right
// primitive for tree-killing on Windows but adds enough complexity that v1
// ships the simpler "kill parent and trust the OS to reap children" path.
func SetProcessGroup(_ *exec.Cmd) {}

// KillProcessGroup just kills the parent on Windows.
func KillProcessGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func setProcessGroup(cmd *exec.Cmd) { SetProcessGroup(cmd) }
func killProcessGroup(pid int) error { return KillProcessGroup(pid) }
