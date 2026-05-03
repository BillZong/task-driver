//go:build !windows

package daemon

import (
	"os/exec"
)

func setHideWindow(cmd *exec.Cmd) {
	// No-op on Unix — no console window to hide
}
