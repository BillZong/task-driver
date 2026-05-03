//go:build windows

package daemon

import (
	"fmt"
	"os/exec"
	"syscall"
)

func setHideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}

// startUnix is a stub on Windows — it should not be called.
func startUnix(bin string, args []string, pidFile, logFile string) error {
	return fmt.Errorf("startUnix is not supported on Windows")
}
