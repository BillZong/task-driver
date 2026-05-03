// Package daemon provides cross-platform daemonization for Task Driver.
//
// On Unix (Linux/macOS), it uses fork+setsid to detach from the terminal.
// On Windows, it uses CREATE_NO_WINDOW to suppress the console window.
//
// Subcommands:
//
//	daemon      — fork and run as daemon
//	start       — fork and run as daemon (alias)
//	stop        — kill the daemon process (via PID file)
//	status      — check if the daemon is running
//	install     — install a system service unit (systemd / launchd)
//	uninstall   — remove the system service unit
package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const (
	// DefaultPIDFile is the default path for the PID file.
	DefaultPIDFile = "~/.config/task-driver/task-driver.pid"
	// DefaultLogFile is the default path for the log file.
	DefaultLogFile = "~/.config/task-driver/task-driver.log"
	// ServiceName is used for systemd/launchd service names.
	ServiceName = "task-driver"
)

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return home + path[1:]
}

// Options holds daemon configuration.
type Options struct {
	// BinaryPath is the path to the task-driver binary. If empty, os.Args[0] is used.
	BinaryPath string
	// ListenAddr is the address the daemon should bind to (e.g. ":9876").
	ListenAddr string
	// DBPath is the database path override (empty means use config default).
	DBPath string
	// ConfigPath is the path to the config YAML file.
	ConfigPath string
	// PIDFile is the path to the PID file. Defaults to DefaultPIDFile.
	PIDFile string
	// LogFile is the path to the log file. Defaults to DefaultLogFile.
	LogFile string
}

func (o *Options) binary() string {
	if o.BinaryPath != "" {
		return o.BinaryPath
	}
	return os.Args[0]
}

func (o *Options) pidFile() string {
	if o.PIDFile != "" {
		return expandHome(o.PIDFile)
	}
	return expandHome(DefaultPIDFile)
}

func (o *Options) logFile() string {
	if o.LogFile != "" {
		return expandHome(o.LogFile)
	}
	return expandHome(DefaultLogFile)
}

func (o *Options) argsForDaemon() []string {
	args := []string{"--config", o.ConfigPath}
	if o.DBPath != "" {
		args = append(args, "--db", o.DBPath)
	}
	return args
}

// Start forks a daemon process and returns immediately.
// The daemon writes its PID to the PID file and redirects stdout/stderr to the log file.
func Start(opts Options) error {
	bin := opts.binary()
	pidFile := opts.pidFile()
	logFile := opts.logFile()

	// Check if already running
	if pid, _ := readPID(pidFile); pid > 0 {
		if isRunning(pid) {
			return fmt.Errorf("task-driver is already running (PID %d)", pid)
		}
		// Stale PID file
		_ = os.Remove(pidFile)
	}

	// Ensure log directory exists
	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}

	// Ensure PID file directory exists
	pidDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return fmt.Errorf("create pid directory: %w", err)
	}

	args := opts.argsForDaemon()

	switch runtime.GOOS {
	case "windows":
		return startWindows(bin, args, pidFile, logFile)
	default:
		return startUnix(bin, args, pidFile, logFile)
	}
}

// startUnix forks a child process via syscall.ForkExec, detaches it from the
// terminal, redirects output to the log file, and writes the PID file.
func startUnix(bin string, args []string, pidFile, logFile string) error {
	log, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer log.Close()

	// ForkExec with the child's stdout/stderr pointing to the log.
	// The child will call setsid to create a new session.
	attr := &syscall.ProcAttr{
		Files: []uintptr{os.Stdin.Fd(), log.Fd(), log.Fd()},
		Env:   os.Environ(),
		Sys: &syscall.SysProcAttr{
			Setsid: true, // detach from terminal
		},
	}

	pid, err := syscall.ForkExec(bin, append([]string{bin}, args...), attr)
	if err != nil {
		return fmt.Errorf("fork exec: %w", err)
	}

	// Write PID file
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	return nil
}

// startWindows starts the daemon with CREATE_NO_WINDOW on Windows.
func startWindows(bin string, args []string, pidFile, logFile string) error {
	log, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer log.Close()

	cmd := exec.Command(bin, args...)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	setHideWindow(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	// Detach — release our reference so the child continues after parent exits.
	_ = cmd.Process.Release()

	return nil
}

// Stop kills the daemon process by reading the PID file and sending SIGTERM.
func Stop(opts Options) error {
	pidFile := opts.pidFile()
	pid, err := readPID(pidFile)
	if err != nil {
		return fmt.Errorf("read pid file (%s): %w", pidFile, err)
	}
	if pid <= 0 {
		return fmt.Errorf("no PID found in %s", pidFile)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		// SIGTERM fallback
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("signal process %d: %w", pid, err)
		}
	}

	_ = os.Remove(pidFile)
	return nil
}

// Status checks whether the daemon is running and returns its PID.
// Returns (0, nil) if not running.
func Status(opts Options) (int, error) {
	pidFile := opts.pidFile()
	pid, err := readPID(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	if pid <= 0 {
		return 0, nil
	}
	if !isRunning(pid) {
		// Clean up stale PID file
		_ = os.Remove(pidFile)
		return 0, nil
	}
	return pid, nil
}

// InstallService installs a system service unit.
func InstallService(opts Options) error {
	switch runtime.GOOS {
	case "linux":
		return installSystemd(opts)
	case "darwin":
		return installLaunchd(opts)
	default:
		return fmt.Errorf("service installation not supported on %s", runtime.GOOS)
	}
}

// UninstallService removes the system service unit.
func UninstallService(opts Options) error {
	switch runtime.GOOS {
	case "linux":
		return uninstallSystemd(opts)
	case "darwin":
		return uninstallLaunchd(opts)
	default:
		return fmt.Errorf("service uninstallation not supported on %s", runtime.GOOS)
	}
}

// installSystemd writes a systemd service unit file and enables it.
func installSystemd(opts Options) error {
	bin := opts.binary()
	unit := fmt.Sprintf(`[Unit]
Description=Task Driver — Agent-agnostic task state persistence
After=network.target

[Service]
Type=simple
ExecStart=%s --config %s
Restart=on-failure
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, bin, opts.ConfigPath, opts.logFile(), opts.logFile())

	unitPath := fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	// Enable
	cmd := exec.Command("systemctl", "enable", ServiceName+".service")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("enable systemd service: %w", err)
	}

	return nil
}

// uninstallSystemd stops and disables the systemd service.
func uninstallSystemd(opts Options) error {
	_ = exec.Command("systemctl", "stop", ServiceName+".service").Run()
	_ = exec.Command("systemctl", "disable", ServiceName+".service").Run()

	unitPath := fmt.Sprintf("/etc/systemd/system/%s.service", ServiceName)
	_ = os.Remove(unitPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()

	return nil
}

// installLaunchd writes a launchd plist and loads it.
func installLaunchd(opts Options) error {
	bin := opts.binary()
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.taskdriver.daemon</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>--config</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, bin, opts.ConfigPath, opts.logFile(), opts.logFile())

	plistPath := filepath.Join(expandHome("~/Library/LaunchAgents"), "com.taskdriver.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("load launchd plist: %w", err)
	}

	return nil
}

// uninstallLaunchd unloads and removes the launchd plist.
func uninstallLaunchd(opts Options) error {
	plistPath := filepath.Join(expandHome("~/Library/LaunchAgents"), "com.taskdriver.daemon.plist")
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	_ = os.Remove(plistPath)
	return nil
}

// ── helpers ──────────────────────────────────────────────────

func readPID(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func isRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 checks if the process exists without actually signaling it.
	if runtime.GOOS != "windows" {
		return proc.Signal(os.Signal(syscall.Signal(0))) == nil
	}
	// On Windows, FindProcess always succeeds, so we try to open the process handle.
	// The simplest approach: assume the process exists. Callers can verify via log.
	_ = proc.Release()
	return true
}

// DaemonizeLogWriter returns an io.Writer that writes to the configured log file.
func DaemonizeLogWriter(logFile string) (io.Writer, error) {
	logPath := expandHome(logFile)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}
