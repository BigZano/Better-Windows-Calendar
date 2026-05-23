package ui

import (
	"fmt"
	"image/color"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/autostart"
	"pycalendar/internal/config"
	"pycalendar/internal/storage"
)

const wizardSteps = 3

// ShowFirstRunWizard opens the one-time setup wizard. Should be called before
// the Fyne event loop (a.Run()) so the window appears on first launch.
func ShowFirstRunWizard() {
	a := getFyneApp()
	w := a.NewWindow("Welcome to PyCalendar — Setup")
	w.Resize(fyne.NewSize(540, 440))

	dbPath, _ := storage.GetDBPath()
	cfgPath, _ := config.GetConfigPath()
	logPath, _ := config.GetLogPath()

	autostartWanted := true
	step := 0

	// ---- navigation bar ----
	stepLbl := widget.NewLabel("Step 1 of 3")
	stepLbl.Alignment = fyne.TextAlignCenter

	backBtn := widget.NewButton("← Back", nil)
	nextBtn := widget.NewButton("Next →", nil)
	nextBtn.Importance = widget.HighImportance

	navBar := container.NewBorder(nil, nil, backBtn, nextBtn,
		container.NewCenter(stepLbl))

	// ---- shared content container ----
	content := container.NewVBox()
	scroll := container.NewVScroll(content)

	// ---- page renderers ----
	renderPage := func() {
		content.Objects = nil

		title := func(text string) fyne.CanvasObject {
			t := canvas.NewText(text, color.RGBA{R: 235, G: 235, B: 235, A: 255})
			t.TextSize = 20
			t.TextStyle = fyne.TextStyle{Bold: true}
			return t
		}
		sep := widget.NewSeparator

		switch step {
		case 0:
			backBtn.Disable()
			nextBtn.SetText("Next →")
			stepLbl.SetText(fmt.Sprintf("Step %d of %d", step+1, wizardSteps))

			body := widget.NewLabel(
				"PyCalendar is your local-first desktop calendar.\n\n" +
					"All your events are stored only on this machine — " +
					"nothing is sent to any cloud service.\n\n" +
					"This short wizard will show you where your data lives " +
					"and let you choose a startup preference.")
			body.Wrapping = fyne.TextWrapWord

			content.Add(title("Welcome to PyCalendar"))
			content.Add(sep())
			content.Add(body)

		case 1:
			backBtn.Enable()
			nextBtn.SetText("Next →")
			stepLbl.SetText(fmt.Sprintf("Step %d of %d", step+1, wizardSteps))

			makeRow := func(heading, path string) fyne.CanvasObject {
				h := widget.NewLabelWithStyle(heading, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
				v := widget.NewLabel(path)
				v.Wrapping = fyne.TextWrapBreak
				return container.NewVBox(h, v, widget.NewSeparator())
			}

			note := widget.NewLabel(
				"Back up or inspect these files at any time.\n" +
					"Log entries are also viewable in Settings → Logs once the app is running.")
			note.Wrapping = fyne.TextWrapWord

			content.Add(title("Where your data lives"))
			content.Add(sep())
			content.Add(makeRow("📁  Database (events & calendars):", dbPath))
			content.Add(makeRow("⚙️  Config file:", cfgPath))
			content.Add(makeRow("📋  Log file:", logPath))
			content.Add(note)

		case 2:
			backBtn.Enable()
			nextBtn.SetText("Finish")
			stepLbl.SetText(fmt.Sprintf("Step %d of %d", step+1, wizardSteps))

			desc := widget.NewLabel(
				"Enabling autostart keeps PyCalendar in the system tray so " +
					"reminders fire even if you haven't opened the app manually.\n\n" +
					"You can change this at any time in Settings → General.")
			desc.Wrapping = fyne.TextWrapWord

			check := widget.NewCheck("Start PyCalendar automatically at login", func(v bool) {
				autostartWanted = v
			})
			check.SetChecked(true)

			content.Add(title("Start with Windows?"))
			content.Add(sep())
			content.Add(desc)
			content.Add(check)
		}

		content.Refresh()
		scroll.ScrollToTop()
	}

	// ---- button actions ----
	finish := func() {
		if autostartWanted {
			execPath, _ := os.Executable()
			_ = autostart.Enable(execPath)
		}
		cfg, _ := config.Load()
		cfg.UI.FirstRunComplete = true
		_ = config.Save(cfg)
		w.Close()
		ShowCalendarWindow()
	}

	nextBtn.OnTapped = func() {
		if step == wizardSteps-1 {
			finish()
			return
		}
		step++
		renderPage()
	}

	backBtn.OnTapped = func() {
		if step > 0 {
			step--
			renderPage()
		}
	}

	// dismiss without completing → still mark done so wizard doesn't re-show
	w.SetCloseIntercept(func() {
		cfg, _ := config.Load()
		cfg.UI.FirstRunComplete = true
		_ = config.Save(cfg)
		w.Close()
	})

	renderPage()

	w.SetContent(container.NewBorder(nil, navBar, nil, nil, scroll))
	w.Show()
}
