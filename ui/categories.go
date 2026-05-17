package ui

import (
	"image/color"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
)

// activeCategoryFilter holds the set of category IDs the user wants to see.
// Empty = no filter (all events shown). Non-empty = show events with at least one match.
var activeCategoryFilter = map[int64]bool{}

// onCategoriesChanged is called after category create/delete so the main window
// can rebuild the filter bar.
var onCategoriesChanged func()

// isEventMatchingFilter returns true when the event passes the active category filter.
func isEventMatchingFilter(e api.Event) bool {
	if len(activeCategoryFilter) == 0 {
		return true
	}
	for _, cat := range e.Categories {
		if activeCategoryFilter[cat.ID] {
			return true
		}
	}
	return false
}

// buildCategoryFilterBar returns a filter row (placed above the tabs in the main window)
// and a rebuild function to call when the category list changes.
// onFilterChange is called whenever the filter selection changes.
func buildCategoryFilterBar(onFilterChange func()) (fyne.CanvasObject, func()) {
	row := container.NewHBox()

	var rebuild func()
	rebuild = func() {
		row.Objects = nil
		cats, err := api.GetCategories()
		if err != nil {
			slog.Error("filter bar: load categories", "err", err)
		}
		if len(cats) == 0 {
			row.Refresh()
			return
		}

		row.Add(widget.NewLabel("Filter:"))

		allBtn := widget.NewButton("All", func() {
			activeCategoryFilter = map[int64]bool{}
			if onFilterChange != nil {
				onFilterChange()
			}
		})
		allBtn.Importance = widget.LowImportance
		row.Add(allBtn)

		for _, cat := range cats {
			c := cat
			dot := canvas.NewRectangle(parseHexColor(c.Color))
			dot.SetMinSize(fyne.NewSize(10, 10))

			check := widget.NewCheck(c.Name, func(on bool) {
				if on {
					activeCategoryFilter[c.ID] = true
				} else {
					delete(activeCategoryFilter, c.ID)
				}
				if onFilterChange != nil {
					onFilterChange()
				}
			})
			check.SetChecked(activeCategoryFilter[c.ID])

			row.Add(container.NewHBox(dot, check))
		}
		row.Refresh()
	}
	rebuild()

	return container.NewHScroll(row), rebuild
}

// buildCategoriesSettingsTab returns the Categories tab content for the Settings window.
func buildCategoriesSettingsTab() fyne.CanvasObject {
	listBox := container.NewVBox()

	var rebuild func()
	rebuild = func() {
		listBox.Objects = nil
		cats, err := api.GetCategories()
		if err != nil {
			slog.Error("categories tab: load", "err", err)
		}
		for _, cat := range cats {
			c := cat
			dot := canvas.NewRectangle(parseHexColor(c.Color))
			dot.SetMinSize(fyne.NewSize(14, 14))

			lbl := widget.NewLabel(c.Name)

			delBtn := widget.NewButton("Delete", func() {
				if err := api.DeleteCategory(c.ID); err != nil {
					slog.Error("delete category failed", "id", c.ID, "err", err)
					return
				}
				delete(activeCategoryFilter, c.ID)
				rebuild()
				if onCategoriesChanged != nil {
					onCategoriesChanged()
				}
			})
			delBtn.Importance = widget.DangerImportance

			listBox.Add(container.NewHBox(dot, lbl, delBtn))
		}
		listBox.Refresh()
	}
	rebuild()

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Category name")

	colorEntry := widget.NewEntry()
	colorEntry.SetText("#6B7280")

	swatch := canvas.NewRectangle(parseHexColor("#6B7280"))
	swatch.SetMinSize(fyne.NewSize(20, 20))
	colorEntry.OnChanged = func(s string) {
		if len(s) == 7 && s[0] == '#' {
			swatch.FillColor = parseHexColor(s)
			swatch.Refresh()
		}
	}

	errorLbl := canvas.NewText("", color.RGBA{R: 200, A: 255})

	addBtn := widget.NewButton("Add", func() {
		name := nameEntry.Text
		if name == "" {
			errorLbl.Text = "Name is required"
			errorLbl.Refresh()
			return
		}
		errorLbl.Text = ""
		_, err := api.CreateCategory(name, colorEntry.Text)
		if err != nil {
			errorLbl.Text = "Failed: " + err.Error()
			errorLbl.Refresh()
			return
		}
		nameEntry.SetText("")
		rebuild()
		if onCategoriesChanged != nil {
			onCategoriesChanged()
		}
	})

	presets := []string{
		"#6B7280", "#3B82F6", "#EF4444", "#22C55E",
		"#F59E0B", "#8B5CF6", "#EC4899", "#14B8A6",
	}
	presetRow := container.NewHBox()
	for _, p := range presets {
		pColor := p
		dot := canvas.NewRectangle(parseHexColor(pColor))
		dot.SetMinSize(fyne.NewSize(20, 20))
		btn := widget.NewButton("", func() {
			colorEntry.SetText(pColor)
			swatch.FillColor = parseHexColor(pColor)
			swatch.Refresh()
		})
		btn.Importance = widget.LowImportance
		presetRow.Add(container.NewStack(dot, btn))
	}

	addForm := container.NewVBox(
		widget.NewLabel("New category:"),
		formRow("Name:", nameEntry),
		widget.NewLabel("Color:"),
		presetRow,
		container.NewHBox(swatch, formRow("Hex:", colorEntry)),
		errorLbl,
		addBtn,
	)

	return container.NewBorder(addForm, nil, nil, nil, container.NewVScroll(listBox))
}

// buildCategoryChecklist returns a scrollable checklist of all categories,
// pre-checked according to the given set. Used in Add/Edit event forms.
func buildCategoryChecklist(checked map[int64]bool) ([]*widget.Check, []int64, fyne.CanvasObject) {
	cats, _ := api.GetCategories()
	checks := make([]*widget.Check, len(cats))
	ids := make([]int64, len(cats))

	box := container.NewVBox()
	for i, cat := range cats {
		c := cat
		check := widget.NewCheck(c.Name, nil)
		check.SetChecked(checked[c.ID])
		checks[i] = check
		ids[i] = c.ID

		dot := canvas.NewRectangle(parseHexColor(c.Color))
		dot.SetMinSize(fyne.NewSize(10, 10))
		box.Add(container.NewHBox(dot, check))
	}

	if len(cats) == 0 {
		return checks, ids, widget.NewLabel("No categories yet — create them in Settings › Categories.")
	}

	scroll := container.NewVScroll(box)
	scroll.SetMinSize(fyne.NewSize(200, 80))
	return checks, ids, scroll
}
