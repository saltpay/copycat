package config

import (
	"os"
	"path/filepath"
)

const (
	AppName        = "copycat"
	ConfigFileName = "config.yaml"
)

// ConfigDir returns the platform-appropriate config directory for copycat.
//   - Linux: ~/.config/copycat
//   - macOS: ~/Library/Application Support/copycat
//   - Windows: %AppData%/copycat
func ConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(configDir, AppName), nil
}

// ConfigPath returns the full path to the XDG config file.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, ConfigFileName), nil
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	return os.MkdirAll(dir, 0o755)
}

// ConfigExists checks if a config file exists at the platform config path.
func ConfigExists() (bool, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return false, "", err
	}

	if _, err := os.Stat(path); err == nil {
		return true, path, nil
	}

	return false, "", nil
}
