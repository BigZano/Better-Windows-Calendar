// Package barsetup detects installed bar applications and injects
// PyCalendar widget/module configuration non-destructively.
package barsetup

// Status values returned per bar application.
const (
	StatusInstalled    = "installed"    // snippet added successfully
	StatusAlreadySetUp = "already_set_up" // pycalendar already present, skipped
	StatusNotFound     = "not_found"    // config file not detected
	StatusError        = "error"        // config found but write failed
)

// Result reports what happened for each supported bar.
type Result struct {
	KomorebiBar string
	Waybar      string
	Polybar     string
}

// HasAny reports whether at least one bar was found and configured.
func (r Result) HasAny() bool {
	return r.KomorebiBar == StatusInstalled || r.Waybar == StatusInstalled || r.Polybar == StatusInstalled
}
