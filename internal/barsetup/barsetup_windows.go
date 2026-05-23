//go:build windows

package barsetup

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RunSetup detects komorebi-bar on Windows and injects a PyCalendar widget.
func RunSetup(execPath string) Result {
	return Result{
		KomorebiBar: setupKomorebiBar(execPath),
	}
}

func komorebiConfigPaths() []string {
	var paths []string
	if local, ok := os.LookupEnv("LOCALAPPDATA"); ok && local != "" {
		paths = append(paths,
			filepath.Join(local, "komorebi-bar", "config.json"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "komorebi-bar", "config.json"),
			filepath.Join(home, "AppData", "Local", "komorebi-bar", "config.json"),
		)
	}
	return paths
}

func setupKomorebiBar(execPath string) string {
	var configPath string
	for _, p := range komorebiConfigPaths() {
		if _, err := os.Stat(p); err == nil {
			configPath = p
			break
		}
	}
	if configPath == "" {
		return StatusNotFound
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		slog.Warn("komorebi-bar: read config failed", "err", err)
		return StatusError
	}

	if strings.Contains(string(data), "pycalendar") {
		return StatusAlreadySetUp
	}

	// Parse as generic JSON so we can modify without schema knowledge.
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Warn("komorebi-bar: parse config failed, falling back to append", "err", err)
		return injectKomorebiAppend(configPath, data, execPath)
	}

	widget := map[string]any{
		"type":    "Custom",
		"command": fmt.Sprintf("%s --mode bar --format json", execPath),
		"label":   "Calendar",
	}

	// Try to add to the first bar configuration's widgets_right.
	inserted := false
	if barsAny, ok := cfg["bar_configurations"]; ok {
		if bars, ok := barsAny.([]any); ok && len(bars) > 0 {
			if bar, ok := bars[0].(map[string]any); ok {
				bar["widgets_right"] = appendWidget(bar["widgets_right"], widget)
				bars[0] = bar
				cfg["bar_configurations"] = bars
				inserted = true
			}
		}
	}
	if !inserted {
		// Fallback: try top-level widgets_right.
		cfg["widgets_right"] = appendWidget(cfg["widgets_right"], widget)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Warn("komorebi-bar: marshal failed", "err", err)
		return StatusError
	}
	if err := os.WriteFile(configPath, out, 0644); err != nil {
		slog.Error("komorebi-bar: write config failed", "err", err)
		return StatusError
	}
	slog.Info("komorebi-bar: inserted PyCalendar widget", "path", configPath)
	return StatusInstalled
}

func appendWidget(existing any, widget map[string]any) []any {
	if arr, ok := existing.([]any); ok {
		return append(arr, widget)
	}
	return []any{widget}
}

// injectKomorebiAppend falls back to appending a commented snippet when the
// config can't be parsed as JSON.
func injectKomorebiAppend(configPath string, original []byte, execPath string) string {
	snippet := fmt.Sprintf("\n// PyCalendar widget — add to your widgets array:\n// {\"type\": \"Custom\", \"command\": \"%s --mode bar --format json\", \"label\": \"Calendar\"}\n", execPath)
	content := strings.TrimRight(string(original), "\n") + snippet
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return StatusError
	}
	return StatusInstalled
}
