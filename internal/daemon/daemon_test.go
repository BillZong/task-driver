package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPID(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "test.pid")

	// Write valid PID
	if err := os.WriteFile(pidFile, []byte("12345\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pid, err := readPID(pidFile)
	if err != nil {
		t.Fatalf("readPID: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("expected 12345, got %d", pid)
	}

	// Missing file
	_, err = readPID(filepath.Join(dir, "missing.pid"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadPID_invalid(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "bad.pid")
	if err := os.WriteFile(pidFile, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readPID(pidFile)
	if err == nil {
		t.Fatal("expected error for invalid PID")
	}
}

func TestOptions_pidFile_default(t *testing.T) {
	opts := Options{}
	got := opts.pidFile()
	if got == "" {
		t.Fatal("pidFile() returned empty string")
	}
	// Should contain task-driver.pid
	if filepath.Base(got) != "task-driver.pid" {
		t.Fatalf("unexpected pid filename: %s", filepath.Base(got))
	}
}

func TestOptions_logFile_default(t *testing.T) {
	opts := Options{}
	got := opts.logFile()
	if got == "" {
		t.Fatal("logFile() returned empty string")
	}
	if filepath.Base(got) != "task-driver.log" {
		t.Fatalf("unexpected log filename: %s", filepath.Base(got))
	}
}

func TestOptions_binary(t *testing.T) {
	// When BinaryPath is set, use it
	opts := Options{BinaryPath: "/custom/path/task-driver"}
	if got := opts.binary(); got != "/custom/path/task-driver" {
		t.Fatalf("expected custom path, got %s", got)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~", home},
		{"~/config/test.yaml", home + "/config/test.yaml"},
		{"/absolute/path", "/absolute/path"},
		{"", ""},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Fatalf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestArgsForDaemon(t *testing.T) {
	opts := Options{
		ConfigPath: "/cfg.yaml",
		DBPath:     "/db.sqlite",
	}
	args := opts.argsForDaemon()
	if len(args) < 2 {
		t.Fatalf("too few args: %v", args)
	}
	if args[0] != "--config" || args[1] != "/cfg.yaml" {
		t.Fatalf("expected --config /cfg.yaml, got %v", args[:2])
	}
	if args[2] != "--db" || args[3] != "/db.sqlite" {
		t.Fatalf("expected --db /db.sqlite, got %v", args[2:4])
	}

	// Without DBPath
	opts2 := Options{ConfigPath: "/cfg.yaml"}
	args2 := opts2.argsForDaemon()
	if len(args2) != 2 {
		t.Fatalf("expected 2 args without DBPath, got %d: %v", len(args2), args2)
	}
}

func TestDaemonizeLogWriter(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "daemon.log")

	w, err := DaemonizeLogWriter(logFile)
	if err != nil {
		t.Fatalf("DaemonizeLogWriter: %v", err)
	}

	_, err = w.Write([]byte("hello daemon\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello daemon\n" {
		t.Fatalf("expected 'hello daemon\\n', got %q", string(data))
	}
}

func TestOptions_pidFile_custom(t *testing.T) {
	opts := Options{PIDFile: "/custom/run/task-driver.pid"}
	got := opts.pidFile()
	if got != "/custom/run/task-driver.pid" {
		t.Fatalf("expected custom pid path, got %s", got)
	}
}

func TestOptions_logFile_custom(t *testing.T) {
	opts := Options{LogFile: "/custom/logs/task-driver.log"}
	got := opts.logFile()
	if got != "/custom/logs/task-driver.log" {
		t.Fatalf("expected custom log path, got %s", got)
	}
}

func TestIsRunning(t *testing.T) {
	// Our own PID should always be running
	if !isRunning(os.Getpid()) {
		t.Fatal("expected own PID to be running")
	}
	// PID 0 is invalid
	if isRunning(0) {
		t.Fatal("PID 0 should not be running")
	}
	// Use a very high PID that likely doesn't exist
	if isRunning(999999999) {
		// On some platforms FindProcess may succeed for high PIDs
		t.Log("Note: isRunning(999999999) returned true (platform quirk)")
	}
}
