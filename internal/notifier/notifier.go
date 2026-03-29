package notifier

import (
	"html"
	"log/slog"

	"github.com/gen2brain/beeep"
)

// Notify sends a desktop notification with the given title and message.
// Both values are HTML-escaped before delivery to prevent XML injection
// in platform notification backends (e.g. Windows Toast XML templates).
func Notify(title, message string) error {
	t := html.EscapeString(title)
	m := html.EscapeString(message)

	if err := beeep.Notify(t, m, ""); err != nil {
		slog.Warn("desktop notification failed, falling back to alert", "err", err)
		return beeep.Alert(t, m, "")
	}
	return nil
}

// Alert sends an urgent/persistent notification (modal on some platforms).
// Input is HTML-escaped for the same reason as Notify.
func Alert(title, message string) error {
	return beeep.Alert(html.EscapeString(title), html.EscapeString(message), "")
}
