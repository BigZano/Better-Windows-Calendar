//go:build windows

package autostart

import (
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName = "PyCalendarDaemon"
)

// Enable writes an autostart registry entry under HKCU\...\Run so the
// daemon launches at login.
func Enable(execPath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry run key: %w", err)
	}
	defer k.Close()

	// Double-quote is illegal in Windows file paths, but strip any defensively
	// so an unexpected execPath never breaks the registry value's cmd quoting.
	safe := strings.ReplaceAll(execPath, `"`, "")
	value := `"` + safe + `" --mode daemon`
	if err := k.SetStringValue(valueName, value); err != nil {
		return fmt.Errorf("write registry value: %w", err)
	}
	slog.Info("autostart enabled", "key", valueName, "value", value)
	return nil
}

// Disable removes the autostart registry entry.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry run key: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(valueName); err != nil {
		return fmt.Errorf("delete registry value: %w", err)
	}
	slog.Info("autostart disabled")
	return nil
}

// IsEnabled reports whether the autostart entry is currently present.
func IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	_, _, err = k.GetStringValue(valueName)
	return err == nil
}
