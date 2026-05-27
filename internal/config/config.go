package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all application configuration, mirroring the TOML structure.
type Config struct {
	Notifications NotificationsConfig `toml:"notifications"`
	MobilePush    MobilePushConfig    `toml:"mobile_push"`
	UI            UIConfig            `toml:"ui"`
	OAuth         OAuthConfig         `toml:"oauth"`
	Sync          SyncConfig          `toml:"sync"`
}

// OAuthConfig holds optional per-provider client ID overrides.
// Built-in defaults are shipped in the binary; these override them for power
// users or enterprise deployments (ADR-0004, ADR-0005).
type OAuthConfig struct {
	MicrosoftClientID string `toml:"microsoft_client_id"`
	GoogleClientID    string `toml:"google_client_id"`
}

// SyncConfig controls the sync engine behaviour (Milestone 2).
type SyncConfig struct {
	// IntervalMinutes is how often SyncAll runs. Default 5, independent of
	// the 30-second reminder daemon interval (ADR-0002).
	IntervalMinutes    int    `toml:"interval_minutes"`
	// ConflictResolution is "remote-wins" (default) or "last-write-wins" (ADR-0007).
	ConflictResolution string `toml:"conflict_resolution"`
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
	Theme               string  `toml:"theme"`               // "system" | "light" | "dark" | "retro"
	DefaultView         string  `toml:"default_view"`        // "day" | "week" | "month"
	ShowWeekNumbers     bool    `toml:"show_week_numbers"`
	MuteInviteCalendars []int64 `toml:"mute_invite_calendars"` // calendar IDs with invite prompts silenced
	HiddenCalendars     []int64 `toml:"hidden_calendars"`      // calendar IDs hidden in all grid views
	FirstRunComplete    bool    `toml:"first_run_complete"`    // true after setup wizard has been shown
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
			Theme:               "system",
			DefaultView:         "day",
			ShowWeekNumbers:     false,
			MuteInviteCalendars: nil,
		},
		Sync: SyncConfig{
			IntervalMinutes:    5,
			ConflictResolution: "remote-wins",
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

// GetLogPath returns the platform-appropriate path for the application log file.
func GetLogPath() (string, error) {
	var base string
	if dir, ok := os.LookupEnv("LOCALAPPDATA"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar", "PyCalendar")
	} else if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share", "PyCalendar")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", fmt.Errorf("cannot create log directory: %w", err)
	}
	return filepath.Join(base, "pycalendar.log"), nil
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

// ValidateWebhookURL returns an error if url is non-empty and not a valid HTTPS URL.
func ValidateWebhookURL(url string) error {
	if url == "" {
		return nil
	}
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("webhook URL must start with https://")
	}
	return nil
}

// Save writes cfg to the config file, creating it if necessary.
// Returns an error if MobilePush is enabled with an invalid webhook URL.
func Save(cfg Config) error {
	if cfg.MobilePush.Enabled {
		if err := ValidateWebhookURL(cfg.MobilePush.WebhookURL); err != nil {
			return fmt.Errorf("invalid mobile push config: %w", err)
		}
	}
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
