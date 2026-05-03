package config

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds the configuration for an agent.
type AgentConfig struct {
	ResumeCmd   string `yaml:"resume_cmd"`
	Description string `yaml:"description"`
}

// Config represents the application configuration.
type Config struct {
	Listen string                `yaml:"listen"`
	DB     string                `yaml:"db"`
	Agents map[string]AgentConfig `yaml:"agents"`
}

// setDefaults applies default values to config fields that are empty.
func setDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = ":9876"
	}
	if cfg.DB == "" {
		cfg.DB = "~/.config/task-driver/tasks.db"
	}
}

// expandHome replaces a leading ~/ or ~ with the current user's home directory.
func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}

	usr, err := user.Current()
	if err != nil {
		return path
	}
	homeDir := usr.HomeDir

	if path == "~" {
		return homeDir
	}

	return filepath.Join(homeDir, path[1:])
}

// Load reads and parses the YAML config file at the given path, applies
// default values, and expands ~ in the DB path to the home directory.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	setDefaults(cfg)

	cfg.DB = expandHome(cfg.DB)

	return cfg, nil
}
