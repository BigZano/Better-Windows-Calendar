package ui

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
	"pycalendar/internal/icsimport"
)

// maxImportFileBytes is the hard size limit enforced before an .ics file is
// parsed. Files larger than this are rejected with a clear error.
const maxImportFileBytes = 10 << 20 // 10MB

// importWarnThreshold is the event count above which the dialog shows a warning
// banner.
const importWarnThreshold = 50

// ShowImportDialog opens the .ics Import Dialog: it lets the user pick a file,
// shows a preview (header, summary, scrollable event list, calendar picker),
// and on confirmation imports the events into the chosen calendar (deduping
// against existing events). Escape closes the dialog without importing; Enter
// does not confirm. defaultCalID selects the initially-picked calendar.
func ShowImportDialog(parentWin fyne.Window, defaultCalID int64) {
	a := getFyneApp()
	if parentWin == nil {
		if wins := a.Driver().AllWindows(); len(wins) > 0 {
			parentWin = wins[0]
		}
	}

	filter := storage.NewExtensionFileFilter([]string{".ics"})

	fileDialog := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil || rc == nil {
			return
		}
		defer rc.Close()

		data, readErr := readImportFile(rc)
		if readErr != nil {
			dialog.ShowError(readErr, parentWin)
			return
		}

		preview, parseErr := parsePreviewBytes(data)
		if parseErr != nil {
			dialog.ShowError(parseErr, parentWin)
			return
		}

		showPreviewWindow(a, preview, defaultCalID)
	}, parentWin)
	fileDialog.SetFilter(filter)
	fileDialog.Show()
}

// readImportFile reads the picked file, enforcing the 10MB limit before
// returning the bytes. The size is checked via the on-disk file when a path is
// available, then again defensively while reading.
func readImportFile(rc fyne.URIReadCloser) ([]byte, error) {
	if path := rc.URI().Path(); path != "" {
		if info, err := os.Stat(path); err == nil && info.Size() > maxImportFileBytes {
			return nil, fmt.Errorf("file is too large (%d MB); the limit is 10 MB", info.Size()/(1<<20))
		}
	}

	// Read up to the limit + 1 byte so we can detect an oversize stream even
	// when the path-based stat above was unavailable.
	data, err := io.ReadAll(io.LimitReader(rc, maxImportFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(data) > maxImportFileBytes {
		return nil, fmt.Errorf("file is too large; the limit is 10 MB")
	}
	return data, nil
}

// parsePreviewBytes parses already-read .ics bytes into a Preview, wrapping any
// parse error in the user-facing "could not read .ics file" message. Shared by
// the file-picker dialog and the path-based file-association entry.
func parsePreviewBytes(data []byte) (*icsimport.Preview, error) {
	preview, err := icsimport.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not read .ics file: %w", err)
	}
	return preview, nil
}

// pendingImportPath holds an .ics path supplied on the command line (a
// double-clicked file when this process is the primary instance) so RunTray can
// open it once the Fyne driver is up. Package-level state mirrors how this
// package already holds fyneApp / calendarWindowReload.
var pendingImportPath string

// SetPendingImportPath records an .ics path to open once the tray's Fyne loop is
// ready. Called from main before RunTray when the primary instance was launched
// with a positional .ics argument.
func SetPendingImportPath(path string) { pendingImportPath = path }

// openPendingImportPath consumes any path set via SetPendingImportPath, opening
// the Import Dialog for it. Called from RunTray after the Fyne app is built.
func openPendingImportPath() {
	if pendingImportPath == "" {
		return
	}
	path := pendingImportPath
	pendingImportPath = ""
	OpenImportPath(path)
}

// OpenImportPath reads an .ics file from disk (enforcing the same 10MB guard as
// the file picker), parses it, and shows the import preview window. It is safe
// to call from any goroutine — including the single-instance pipe server — as
// all Fyne UI work is marshalled onto the main loop via fyne.Do.
func OpenImportPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}

	data, err := readImportPath(path)
	if err == nil {
		var preview *icsimport.Preview
		preview, err = parsePreviewBytes(data)
		if err == nil {
			a := getFyneApp()
			fyne.Do(func() { showPreviewWindow(a, preview, 0) })
			return
		}
	}

	// Surface read/parse errors on an available window, or a fresh one.
	slog.Warn("open import path failed", "path", path, "err", err)
	fyne.Do(func() {
		a := getFyneApp()
		var parent fyne.Window
		if wins := a.Driver().AllWindows(); len(wins) > 0 {
			parent = wins[0]
		} else {
			parent = a.NewWindow("Import .ics")
			parent.Resize(fyne.NewSize(420, 160))
			parent.Show()
		}
		dialog.ShowError(err, parent)
	})
}

// readImportPath reads an .ics file by filesystem path, enforcing the 10MB limit
// before reading the full contents.
func readImportPath(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	if info.Size() > maxImportFileBytes {
		return nil, fmt.Errorf("file is too large (%d MB); the limit is 10 MB", info.Size()/(1<<20))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return data, nil
}

func showPreviewWindow(a fyne.App, preview *icsimport.Preview, defaultCalID int64) {
	w := a.NewWindow("Import .ics")
	w.Resize(fyne.NewSize(520, 560))

	// ---- header ----
	headerLines := []fyne.CanvasObject{
		widget.NewLabelWithStyle("Import events", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	}
	if preview.ProdID != "" {
		headerLines = append(headerLines, widget.NewLabel("Source: "+preview.ProdID))
	}
	if preview.Organizer != "" {
		headerLines = append(headerLines, widget.NewLabel("Organizer: "+preview.Organizer))
	}
	headerLines = append(headerLines, widget.NewLabel(importSummaryLine(preview)))

	if preview.Count > importWarnThreshold {
		warn := canvas.NewText(
			fmt.Sprintf("⚠ Large import: %d events. This may take a moment.", preview.Count),
			color.RGBA{R: 220, G: 160, B: 40, A: 255},
		)
		warn.TextStyle = fyne.TextStyle{Bold: true}
		headerLines = append(headerLines, warn)
	}
	headerLines = append(headerLines, widget.NewSeparator())
	header := container.NewVBox(headerLines...)

	// ---- event list ----
	listBox := container.NewVBox()
	for _, e := range preview.Events {
		if e.Title == "" || e.Start.IsZero() {
			continue
		}
		when := e.Start.Format("Jan 2, 2006")
		if !e.AllDay {
			when = e.Start.Format("Jan 2, 2006 15:04")
		}
		listBox.Add(widget.NewLabel(fmt.Sprintf("%s  —  %s", when, e.Title)))
	}
	if len(listBox.Objects) == 0 {
		listBox.Add(widget.NewLabel("(no importable events found)"))
	}

	// ---- calendar picker (defaults to Local) ----
	cals, _ := api.GetCalendars()
	calNames := make([]string, len(cals))
	calIDs := make([]int64, len(cals))
	defaultIdx := 0
	for i, c := range cals {
		calNames[i] = c.Name
		calIDs[i] = c.ID
		if c.ID == defaultCalID || (defaultCalID <= 0 && c.Name == "Local") {
			defaultIdx = i
		}
	}
	if len(calNames) == 0 {
		calNames = []string{"Local"}
		calIDs = []int64{1}
	}
	calSelect := widget.NewSelect(calNames, nil)
	calSelect.SetSelectedIndex(defaultIdx)

	status := canvas.NewText("", color.RGBA{R: 200, G: 200, B: 200, A: 255})

	// ---- buttons ----
	cancelBtn := widget.NewButton("Cancel", func() { w.Close() })
	importBtn := widget.NewButton("Import", func() {
		calID := calIDs[0]
		if idx := calSelect.SelectedIndex(); idx >= 0 && idx < len(calIDs) {
			calID = calIDs[idx]
		}

		res, err := icsimport.Commit(preview, calID)
		if err != nil {
			status.Color = color.RGBA{R: 220, G: 60, B: 60, A: 255}
			status.Text = "Import failed: " + err.Error()
			status.Refresh()
			return
		}

		status.Color = color.RGBA{R: 80, G: 200, B: 80, A: 255}
		status.Text = importReport(res)
		status.Refresh()
		slog.Info("ics import committed",
			"imported", res.Imported, "skipped", res.Skipped, "duplicates", res.Duplicates)

		if res.Imported > 0 && calendarWindowReload != nil {
			calendarWindowReload()
		}
	})
	importBtn.Importance = widget.HighImportance

	footer := container.NewVBox(
		widget.NewSeparator(),
		formRow("Add to calendar:", calSelect),
		container.NewHBox(importBtn, cancelBtn),
		status,
	)

	content := container.NewBorder(header, footer, nil, nil, container.NewVScroll(listBox))
	w.SetContent(content)

	// Escape closes without importing; Enter must not confirm.
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			w.Close()
		}
	})

	w.Show()
}

// importSummaryLine builds a line like "47 events, Jan 2025 – Dec 2025".
func importSummaryLine(p *icsimport.Preview) string {
	noun := "events"
	if p.Count == 1 {
		noun = "event"
	}
	if p.Count == 0 || p.SpanStart.IsZero() {
		return fmt.Sprintf("%d %s", p.Count, noun)
	}
	const monthYear = "Jan 2006"
	start := p.SpanStart.Format(monthYear)
	end := p.SpanEnd.Format(monthYear)
	if start == end {
		return fmt.Sprintf("%d %s, %s", p.Count, noun, start)
	}
	return fmt.Sprintf("%d %s, %s – %s", p.Count, noun, start, end)
}

// importReport builds the post-import message, e.g.
// "Imported 44, 3 skipped (already exist)".
func importReport(res icsimport.Result) string {
	msg := fmt.Sprintf("Imported %d", res.Imported)
	if res.Duplicates > 0 {
		msg += fmt.Sprintf(", %d skipped (already exist)", res.Duplicates)
	}
	if res.Skipped > 0 {
		msg += fmt.Sprintf(", %d skipped (incomplete)", res.Skipped)
	}
	if len(res.Errors) > 0 {
		msg += fmt.Sprintf(", %d failed (see logs)", len(res.Errors))
	}
	return msg
}
