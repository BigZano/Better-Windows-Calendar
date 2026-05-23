//go:build linux

package barsetup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RunSetup detects Waybar and Polybar on Linux and injects PyCalendar modules.
func RunSetup(execPath string) Result {
	return Result{
		Waybar:  setupWaybar(execPath),
		Polybar: setupPolybar(execPath),
	}
}

// ---- Waybar ----

func waybarConfigPaths() []string {
	var paths []string
	if dir, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && dir != "" {
		paths = append(paths,
			filepath.Join(dir, "waybar", "config"),
			filepath.Join(dir, "waybar", "config.json"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "waybar", "config"),
			filepath.Join(home, ".config", "waybar", "config.json"),
		)
	}
	return paths
}

const waybarModule = `
"custom/pycalendar": {
    "exec": "%s --mode bar --format json",
    "format": "{}",
    "return-type": "json",
    "interval": 60,
    "tooltip": true
},
`

func setupWaybar(execPath string) string {
	var configPath string
	for _, p := range waybarConfigPaths() {
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
		return StatusError
	}
	content := string(data)

	if strings.Contains(content, "pycalendar") {
		return StatusAlreadySetUp
	}

	// Inject the module definition before the last closing brace of the root object.
	snippet := fmt.Sprintf(waybarModule, execPath)

	// Try to insert the module definition into the root object.
	// Also try to add "custom/pycalendar" to modules-right if present.
	content = injectWaybarModuleDef(content, snippet)
	content = injectWaybarModuleRef(content)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		slog.Error("waybar: write config failed", "err", err)
		return StatusError
	}
	slog.Info("waybar: inserted pycalendar module", "path", configPath)
	return StatusInstalled
}

// injectWaybarModuleDef inserts the module JSON block before the last } in the file.
func injectWaybarModuleDef(content, snippet string) string {
	// Find the last closing brace (root object end) and insert before it.
	idx := strings.LastIndex(content, "}")
	if idx == -1 {
		return content + snippet
	}
	// Ensure there's a comma before the new entry if there's preceding content.
	prefix := strings.TrimRight(content[:idx], " \t\n")
	if len(prefix) > 0 && !strings.HasSuffix(prefix, ",") && !strings.HasSuffix(prefix, "{") {
		prefix += ","
	}
	return prefix + "\n" + snippet + "\n" + content[idx:]
}

// injectWaybarModuleRef adds "custom/pycalendar" to the modules-right array.
func injectWaybarModuleRef(content string) string {
	targets := []string{`"modules-right"`, `"modules-center"`, `"modules-left"`}
	for _, t := range targets {
		idx := strings.Index(content, t)
		if idx == -1 {
			continue
		}
		// Find the opening [ after the key.
		openIdx := strings.Index(content[idx:], "[")
		if openIdx == -1 {
			continue
		}
		openIdx += idx + 1
		return content[:openIdx] + `"custom/pycalendar", ` + content[openIdx:]
	}
	return content
}

// ---- Polybar ----

func polybarConfigPaths() []string {
	var paths []string
	if dir, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && dir != "" {
		paths = append(paths,
			filepath.Join(dir, "polybar", "config.ini"),
			filepath.Join(dir, "polybar", "config"),
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".config", "polybar", "config.ini"),
			filepath.Join(home, ".config", "polybar", "config"),
		)
	}
	return paths
}

const polybarModule = `
[module/pycalendar]
type = custom/script
exec = %s --mode bar --format polybar
interval = 60
label = %%{F#88c0d0}%%{F-}  %%output%%
`

func setupPolybar(execPath string) string {
	var configPath string
	for _, p := range polybarConfigPaths() {
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
		return StatusError
	}
	content := string(data)

	if strings.Contains(content, "pycalendar") {
		return StatusAlreadySetUp
	}

	// Append the module section and inject module ref into the first bar's modules-right.
	snippet := fmt.Sprintf(polybarModule, execPath)
	content = injectPolybarModuleRef(content)
	content += snippet

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		slog.Error("polybar: write config failed", "err", err)
		return StatusError
	}
	slog.Info("polybar: inserted pycalendar module", "path", configPath)
	return StatusInstalled
}

// injectPolybarModuleRef adds pycalendar to the first modules-right line found.
func injectPolybarModuleRef(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "modules-right") || strings.HasPrefix(trimmed, "modules-center") {
			// Append pycalendar to the modules list.
			lines[i] = strings.TrimRight(line, " \t") + " pycalendar"
			break
		}
	}
	return strings.Join(lines, "\n")
}
