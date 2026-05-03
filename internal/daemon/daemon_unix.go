//go:build darwin || linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func setHideWindow(cmd *exec.Cmd) {
	// No-op on Unix — no console window to hide
}

// startUnix forks a child process via syscall.ForkExec, detaches it from the
// terminal, redirects output to the log file, and writes the PID file.
func startUnix(bin string, args []string, pidFile, logFile string) error {
	log, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer log.Close()

	attr := &syscall.ProcAttr{
		Files: []uintptr{os.Stdin.Fd(), log.Fd(), log.Fd()},
		Env:   os.Environ(),
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	pid, err := syscall.ForkExec(bin, append([]string{bin}, args...), attr)
	if err != nil {
		return fmt.Errorf("fork exec: %w", err)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	return nil
}
