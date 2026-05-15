package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
)

var (
	monthHeaderCol  = color.RGBA{R: 120, G: 120, B: 120, A: 255}
	monthTodayCol   = color.RGBA{R: 50, G: 80, B: 140, A: 255}
	monthCellBgCol  = color.RGBA{R: 25, G: 25, B: 25, A: 255}
	monthEventCol   = color.RGBA{R: 80, G: 130, B: 200, A: 255}
)

var weekdayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// MonthWidget renders a traditional month grid. Clicking a day calls onDayTap.
type MonthWidget struct {
	widget.BaseWidget
	year, month int
	events      []api.Event
	onDayTap    func(time.Time)
}

func NewMonthWidget(year, month int, events []api.Event, onDayTap func(time.Time)) *MonthWidget {
	m := &MonthWidget{year: year, month: month, events: events, onDayTap: onDayTap}
	m.ExtendBaseWidget(m)
	return m
}

func (m *MonthWidget) Reload(year, month int, events []api.Event) {
	m.year = year
	m.month = month
	m.events = events
	m.Refresh()
}

func (m *MonthWidget) CreateRenderer() fyne.WidgetRenderer {
	return &monthRenderer{mw: m}
}

func (m *MonthWidget) MinSize() fyne.Size {
	return fyne.NewSize(560, 480)
}

// ---- renderer ----

type monthRenderer struct {
	mw      *MonthWidget
	objects []fyne.CanvasObject
}

func (r *monthRenderer) Layout(size fyne.Size) { r.rebuild(size) }
func (r *monthRenderer) MinSize() fyne.Size    { return r.mw.MinSize() }
func (r *monthRenderer) Refresh()              { canvas.Refresh(r.mw) }
func (r *monthRenderer) Destroy()              {}
func (r *monthRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *monthRenderer) rebuild(size fyne.Size) {
	r.objects = nil

	// Background
	bg := canvas.NewRectangle(color.RGBA{R: 18, G: 18, B: 18, A: 255})
	bg.Move(fyne.NewPos(0, 0))
	bg.Resize(size)
	r.objects = append(r.objects, bg)

	cellW := size.Width / 7
	headerH := float32(24)

	// Weekday headers
	for i, name := range weekdayNames {
		lbl := canvas.NewText(name, monthHeaderCol)
		lbl.TextSize = 12
		lbl.Move(fyne.NewPos(float32(i)*cellW+4, 4))
		r.objects = append(r.objects, lbl)
	}

	// Determine grid dimensions
	firstDay := time.Date(r.mw.year, time.Month(r.mw.month), 1, 0, 0, 0, 0, time.Local)
	startOffset := int(firstDay.Weekday())
	daysInMonth := daysIn(r.mw.year, r.mw.month)
	numRows := (startOffset + daysInMonth + 6) / 7
	cellH := (size.Height - headerH) / float32(numRows)

	now := time.Now()

	for day := 1; day <= daysInMonth; day++ {
		slot := startOffset + day - 1
		row := slot / 7
		col := slot % 7

		x := float32(col) * cellW
		y := headerH + float32(row)*cellH

		isToday := r.mw.year == now.Year() && int(time.Month(r.mw.month)) == int(now.Month()) && day == now.Day()

		// Cell background
		var cellBg color.RGBA
		if isToday {
			cellBg = monthTodayCol
		} else {
			cellBg = monthCellBgCol
		}
		cell := canvas.NewRectangle(cellBg)
		cell.Move(fyne.NewPos(x+1, y+1))
		cell.Resize(fyne.NewSize(cellW-2, cellH-2))
		r.objects = append(r.objects, cell)

		// Day number
		dayLbl := canvas.NewText(fmt.Sprintf("%d", day), color.RGBA{R: 220, G: 220, B: 220, A: 255})
		dayLbl.TextSize = 12
		dayLbl.Move(fyne.NewPos(x+4, y+4))
		r.objects = append(r.objects, dayLbl)

		// Event dots/titles (up to 3)
		dayDate := time.Date(r.mw.year, time.Month(r.mw.month), day, 0, 0, 0, 0, time.Local)
		dayEvts := eventsForDay(r.mw.events, dayDate)
		maxShow := 3
		if int(cellH) < 50 {
			maxShow = 1
		}
		for i, e := range dayEvts {
			if i >= maxShow {
				more := canvas.NewText(fmt.Sprintf("+%d more", len(dayEvts)-maxShow), monthHeaderCol)
				more.TextSize = 10
				more.Move(fyne.NewPos(x+4, y+18+float32(i)*12))
				r.objects = append(r.objects, more)
				break
			}
			dot := canvas.NewRectangle(monthEventCol)
			dot.Move(fyne.NewPos(x+4, y+18+float32(i)*12))
			dot.Resize(fyne.NewSize(6, 6))
			r.objects = append(r.objects, dot)

			evtLbl := canvas.NewText(truncate(e.Title, 12), color.RGBA{R: 200, G: 200, B: 200, A: 255})
			evtLbl.TextSize = 10
			evtLbl.Move(fyne.NewPos(x+14, y+18+float32(i)*12))
			r.objects = append(r.objects, evtLbl)
		}
	}
}

// buildMonthView returns a container with navigation controls, the month widget,
// and an external reload function that refreshes the currently displayed month.
func buildMonthView(year, month int, events []api.Event) (fyne.CanvasObject, func()) {
	mw := NewMonthWidget(year, month, events, nil)

	reloadAt := func(y, mo int) {
		start := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.Local).Unix()
		end := time.Date(y, time.Month(mo+1), 1, 0, 0, 0, 0, time.Local).Unix()
		evts, _ := api.GetEvents(start, end)
		mw.Reload(y, mo, evts)
	}

	// externalReload uses the captured year/month vars so it always reloads the
	// currently displayed month even after the user navigates.
	externalReload := func() { reloadAt(year, month) }

	title := widget.NewLabel(fmt.Sprintf("%s %d", time.Month(month).String(), year))

	prevBtn := widget.NewButton("◀", func() {
		month--
		if month < 1 {
			month = 12
			year--
		}
		title.SetText(fmt.Sprintf("%s %d", time.Month(month).String(), year))
		reloadAt(year, month)
	})
	nextBtn := widget.NewButton("▶", func() {
		month++
		if month > 12 {
			month = 1
			year++
		}
		title.SetText(fmt.Sprintf("%s %d", time.Month(month).String(), year))
		reloadAt(year, month)
	})

	nav := container.NewHBox(prevBtn, title, nextBtn)
	return container.NewBorder(nav, nil, nil, nil, container.NewVScroll(mw)), externalReload
}

func daysIn(year, month int) int {
	return time.Date(year, time.Month(month+1), 0, 0, 0, 0, 0, time.UTC).Day()
}
