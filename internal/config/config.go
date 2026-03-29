package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds all application configuration, mirroring the TOML structure.
type Config struct {
	Notifications NotificationsConfig `toml:"notifications"`
	MobilePush    MobilePushConfig    `toml:"mobile_push"`
	UI            UIConfig            `toml:"ui"`
}

type NotificationsConfig struct {
	DesktopEnabled         bool `toml:"desktop_enabled"`
	SoundEnabled           bool `toml:"sound_enabled"`
	DefaultReminderMinutes int  `toml:"default_reminder_minutes"`
}

type MobilePushConfig struct {
	Enabled    bool   `toml:"enabled"`
	WebhookURL string `toml:"webhook_url"`
}

type UIConfig struct {
	Theme           string `toml:"theme"`
	ShowWeekNumbers bool   `toml:"show_week_numbers"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Notifications: NotificationsConfig{
			DesktopEnabled:         true,
			SoundEnabled:           true,
			DefaultReminderMinutes: 15,
		},
		MobilePush: MobilePushConfig{
			Enabled:    false,
			WebhookURL: "",
		},
		UI: UIConfig{
			Theme:           "retro",
			ShowWeekNumbers: true,
		},
	}
}

// GetConfigPath returns the platform-appropriate config file path, creating
// the parent directory if necessary.
func GetConfigPath() (string, error) {
	var base string
	if dir, ok := os.LookupEnv("LOCALAPPDATA"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar", "PyCalendar")
	} else if dir, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		base = filepath.Join(home, ".config", "PyCalendar")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", fmt.Errorf("cannot create config directory: %w", err)
	}
	return filepath.Join(base, "config.toml"), nil
}

// Load reads the config file, returning defaults if it does not exist.
func Load() (Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return Default(), err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Info("config file not found, using defaults")
		cfg := Default()
		// Write defaults so the user has a file to edit.
		if writeErr := Save(cfg); writeErr != nil {
			slog.Warn("could not write default config", "err", writeErr)
		}
		return cfg, nil
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		slog.Error("failed to parse config, using defaults", "err", err)
		return Default(), nil
	}

	slog.Info("loaded config", "path", path)
	return cfg, nil
}

// Save writes cfg to the config file, creating it if necessary.
func Save(cfg Config) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("open config for write: %w", err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	slog.Info("saved config", "path", path)
	return nil
}
