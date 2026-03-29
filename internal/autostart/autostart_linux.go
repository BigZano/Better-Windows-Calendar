//go:build linux

package autostart

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

const serviceTemplate = `[Unit]
Description=PyCalendar Notification Daemon

[Service]
ExecStart=%s --mode daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

func serviceDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func servicePath() (string, error) {
	dir, err := serviceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pycalendar-daemon.service"), nil
}

// Enable writes a systemd user service unit and runs `systemctl --user enable`.
func Enable(execPath string) error {
	path, err := servicePath()
	if err != nil {
		return fmt.Errorf("resolve service path: %w", err)
	}

	content := fmt.Sprintf(serviceTemplate, execPath)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}

	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		slog.Warn("daemon-reload failed", "out", string(out), "err", err)
	}

	out, err := exec.Command("systemctl", "--user", "enable", "pycalendar-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl enable: %w (output: %s)", err, out)
	}

	slog.Info("autostart enabled via systemd", "service", path)
	return nil
}

// Disable disables and removes the systemd user service unit.
func Disable() error {
	path, err := servicePath()
	if err != nil {
		return fmt.Errorf("resolve service path: %w", err)
	}

	out, err := exec.Command("systemctl", "--user", "disable", "pycalendar-daemon").CombinedOutput()
	if err != nil {
		slog.Warn("systemctl disable failed", "out", string(out), "err", err)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service file: %w", err)
	}

	slog.Info("autostart disabled")
	return nil
}

// IsEnabled reports whether the service unit file exists.
func IsEnabled() bool {
	path, err := servicePath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}
