//go:build linux

package autostart

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

const daemonServiceTemplate = `[Unit]
Description=PyCalendar Notification Daemon

[Service]
ExecStart=%s --mode daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

const trayDesktopTemplate = `[Desktop Entry]
Type=Application
Name=PyCalendar
Comment=Calendar with tray icon
Exec=%s --mode tray
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
`

// HasDisplay reports whether a graphical display session is available.
func HasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// Enable registers autostart using the mechanism appropriate for the runtime environment:
//   - With display: XDG autostart .desktop file targeting --mode tray
//   - Headless: systemd user service targeting --mode daemon
func Enable(execPath string) error {
	if HasDisplay() {
		return enableXDG(execPath)
	}
	return enableSystemd(execPath)
}

// Disable removes both autostart entries (handles switching between display/headless modes).
func Disable() error {
	_ = disableSystemd()
	_ = disableXDG()
	return nil
}

// IsEnabled reports whether either autostart entry exists.
func IsEnabled() bool {
	if p, err := xdgPath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	if p, err := systemdServicePath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// ---- XDG autostart (with display) ----

func xdgDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "autostart")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func xdgPath() (string, error) {
	dir, err := xdgDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pycalendar.desktop"), nil
}

func enableXDG(execPath string) error {
	path, err := xdgPath()
	if err != nil {
		return fmt.Errorf("resolve XDG autostart path: %w", err)
	}
	content := fmt.Sprintf(trayDesktopTemplate, execPath)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("write desktop file: %w", err)
	}
	slog.Info("autostart enabled via XDG desktop file", "path", path)
	return nil
}

func disableXDG() error {
	path, err := xdgPath()
	if err != nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove desktop file: %w", err)
	}
	return nil
}

// ---- systemd user service (headless) ----

func systemdServiceDir() (string, error) {
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

func systemdServicePath() (string, error) {
	dir, err := systemdServiceDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pycalendar-daemon.service"), nil
}

func enableSystemd(execPath string) error {
	path, err := systemdServicePath()
	if err != nil {
		return fmt.Errorf("resolve service path: %w", err)
	}
	content := fmt.Sprintf(daemonServiceTemplate, execPath)
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

func disableSystemd() error {
	path, err := systemdServicePath()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if out, err := exec.Command("systemctl", "--user", "disable", "pycalendar-daemon").CombinedOutput(); err != nil {
		slog.Warn("systemctl disable failed", "out", string(out), "err", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service file: %w", err)
	}
	slog.Info("autostart disabled")
	return nil
}
