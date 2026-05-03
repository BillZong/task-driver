package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_defaults(t *testing.T) {
	// Create a minimal config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("listen: \":9999\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != ":9999" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9999")
	}

	// DB default
	if cfg.DB == "" {
		t.Error("DB should have default value")
	}

	// Compression defaults
	if cfg.Compression == nil {
		t.Fatal("Compression should have default values")
	}
	if cfg.Compression.Threshold != 0.50 {
		t.Errorf("Threshold = %f, want 0.50", cfg.Compression.Threshold)
	}
	if cfg.Compression.ContextWindow != 1048576 {
		t.Errorf("ContextWindow = %d, want 1048576", cfg.Compression.ContextWindow)
	}
	if tol := cfg.Compression.ThresholdTokens(); tol != 524288 {
		t.Errorf("ThresholdTokens() = %d, want 524288", tol)
	}
}

func TestLoad_full(t *testing.T) {
	yaml := `
listen: ":9876"
db: "/tmp/test.db"
compression:
  threshold: 0.30
  context_window: 128000
agents:
  hermes:
    resume_cmd: "hermes run --resume {session_id}"
    description: "Hermes"
`
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Listen != ":9876" {
		t.Errorf("Listen = %q", ":9876")
	}
	if cfg.Compression.Threshold != 0.30 {
		t.Errorf("Threshold = %f, want 0.30", cfg.Compression.Threshold)
	}
	if cfg.Compression.ContextWindow != 128000 {
		t.Errorf("ContextWindow = %d, want 128000", cfg.Compression.ContextWindow)
	}
	if tol := cfg.Compression.ThresholdTokens(); tol != 38400 {
		t.Errorf("ThresholdTokens() = %d, want 38400", tol)
	}
	if cfg.Agents["hermes"].ResumeCmd != "hermes run --resume {session_id}" {
		t.Errorf("ResumeCmd = %q", cfg.Agents["hermes"].ResumeCmd)
	}
}

func TestLoad_missing_file(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load() should error on missing file")
	}
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"~/test.db", ""}, // we'll check prefix only since home dir varies
		{"/absolute/path.db", "/absolute/path.db"},
		{"relative/path.db", "relative/path.db"},
		{"", ""},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if tt.input == "~/test.db" {
			// Ensure ~ is expanded to an absolute path starting with /
			if got == tt.input {
				t.Errorf("expandHome(%q) was not expanded", tt.input)
			}
		} else if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestThresholdTokens_edge(t *testing.T) {
	c := &CompressionConfig{Threshold: 0, ContextWindow: 1000}
	if tol := c.ThresholdTokens(); tol != 0 {
		t.Errorf("zero threshold: got %d, want 0", tol)
	}
	c = &CompressionConfig{Threshold: 0.5, ContextWindow: 0}
	if tol := c.ThresholdTokens(); tol != 0 {
		t.Errorf("zero window: got %d, want 0", tol)
	}
	c = &CompressionConfig{Threshold: -1, ContextWindow: 1000}
	if tol := c.ThresholdTokens(); tol != 0 {
		t.Errorf("negative threshold: got %d, want 0", tol)
	}
}
