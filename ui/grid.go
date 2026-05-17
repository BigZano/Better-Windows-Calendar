package ui

import (
	"fmt"
	"image/color"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
)

const (
	hourHeight    = float32(60)  // pixels per hour
	labelWidth    = float32(52)  // left gutter for hour labels
	minColWidth   = float32(120)
	popOutWidth   = float32(80)  // blocks narrower than this show "…" and open pop-out
)

var (
	gridLineCol  = color.RGBA{R: 60, G: 60, B: 60, A: 255}
	gridLabelCol = color.RGBA{R: 160, G: 160, B: 160, A: 255}
	nowLineCol   = color.RGBA{R: 255, G: 80, B: 80, A: 200}
	eventTextCol = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// TimeGridWidget is a custom canvas-based widget that renders a scrollable
// time grid for one or more dates (Day = 1 date, Week = 7 dates).
type TimeGridWidget struct {
	widget.BaseWidget
	dates     []time.Time
	events    []api.Event
	calendars map[int64]api.Calendar
	onTap     func(api.Event)
	onPopOut  func([]api.Event)

	// built during Layout so Tapped can hit-test
	eventRects []eventRect
}

type eventRect struct {
	event    api.Event
	groupEvt []api.Event // non-nil when block is narrow → clicking opens pop-out
	x, y     float32
	w, h     float32
}

// NewTimeGridWidget creates a ready-to-use grid widget.
// onPopOut is called with the full overlap group when a narrow block (<80px) is tapped.
func NewTimeGridWidget(dates []time.Time, events []api.Event, cals []api.Calendar, onTap func(api.Event), onPopOut func([]api.Event)) *TimeGridWidget {
	calMap := make(map[int64]api.Calendar, len(cals))
	for _, c := range cals {
		calMap[c.ID] = c
	}
	g := &TimeGridWidget{
		dates:     dates,
		events:    events,
		calendars: calMap,
		onTap:     onTap,
		onPopOut:  onPopOut,
	}
	g.ExtendBaseWidget(g)
	return g
}

// Reload replaces the event/calendar data and redraws.
func (g *TimeGridWidget) Reload(events []api.Event, cals []api.Calendar) {
	calMap := make(map[int64]api.Calendar, len(cals))
	for _, c := range cals {
		calMap[c.ID] = c
	}
	g.events = events
	g.calendars = calMap
	g.Refresh()
}

func (g *TimeGridWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &timeGridRenderer{grid: g}
	return r
}

// MinSize — 24 hours tall, wide enough for all day columns.
func (g *TimeGridWidget) MinSize() fyne.Size {
	cols := float32(len(g.dates))
	if cols < 1 {
		cols = 1
	}
	return fyne.NewSize(labelWidth+minColWidth*cols, hourHeight*24)
}

func (g *TimeGridWidget) Tapped(ev *fyne.PointEvent) {
	px, py := ev.Position.X, ev.Position.Y
	for _, er := range g.eventRects {
		if px >= er.x && px <= er.x+er.w && py >= er.y && py <= er.y+er.h {
			if er.groupEvt != nil && g.onPopOut != nil {
				g.onPopOut(er.groupEvt)
			} else if g.onTap != nil {
				g.onTap(er.event)
			}
			return
		}
	}
}

// ---- renderer ----

type timeGridRenderer struct {
	grid    *TimeGridWidget
	objects []fyne.CanvasObject
}

func (r *timeGridRenderer) Layout(size fyne.Size) {
	r.rebuild(size)
}

func (r *timeGridRenderer) MinSize() fyne.Size { return r.grid.MinSize() }

func (r *timeGridRenderer) Refresh() {
	canvas.Refresh(r.grid)
}

func (r *timeGridRenderer) Destroy() {}

func (r *timeGridRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *timeGridRenderer) rebuild(size fyne.Size) {
	r.objects = nil
	r.grid.eventRects = nil

	numCols := len(r.grid.dates)
	if numCols == 0 {
		return
	}
	colWidth := (size.Width - labelWidth) / float32(numCols)

	// Background
	bg := canvas.NewRectangle(color.RGBA{R: 18, G: 18, B: 18, A: 255})
	bg.Move(fyne.NewPos(0, 0))
	bg.Resize(size)
	r.objects = append(r.objects, bg)

	// Hour lines + labels
	for h := 0; h <= 24; h++ {
		y := float32(h) * hourHeight
		line := canvas.NewRectangle(gridLineCol)
		line.Move(fyne.NewPos(labelWidth, y))
		line.Resize(fyne.NewSize(size.Width-labelWidth, 1))
		r.objects = append(r.objects, line)

		if h < 24 {
			lbl := canvas.NewText(fmt.Sprintf("%02d:00", h), gridLabelCol)
			lbl.TextSize = 11
			lbl.Move(fyne.NewPos(2, y+2))
			r.objects = append(r.objects, lbl)
		}
	}

	// Vertical column separators
	for col := 0; col <= numCols; col++ {
		x := labelWidth + float32(col)*colWidth
		sep := canvas.NewRectangle(gridLineCol)
		sep.Move(fyne.NewPos(x, 0))
		sep.Resize(fyne.NewSize(1, size.Height))
		r.objects = append(r.objects, sep)
	}

	// Day header labels
	for col, d := range r.grid.dates {
		x := labelWidth + float32(col)*colWidth
		hdr := canvas.NewText(d.Format("Mon 01/02"), gridLabelCol)
		hdr.TextSize = 12
		hdr.Move(fyne.NewPos(x+4, -18))
		r.objects = append(r.objects, hdr)
	}

	// Events
	for col, d := range r.grid.dates {
		dayEvents := eventsForDay(r.grid.events, d)
		colX := labelWidth + float32(col)*colWidth
		r.renderDayEvents(dayEvents, colX, colWidth)
	}

	// "Now" line
	now := time.Now()
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for col, d := range r.grid.dates {
		if sameDay(d, now) {
			sinceStart := now.Sub(todayMidnight)
			y := float32(sinceStart.Hours()) * hourHeight
			colX := labelWidth + float32(col)*colWidth
			nl := canvas.NewRectangle(nowLineCol)
			nl.Move(fyne.NewPos(colX, y))
			nl.Resize(fyne.NewSize(colWidth, 2))
			r.objects = append(r.objects, nl)
		}
	}
}

func (r *timeGridRenderer) renderDayEvents(events []api.Event, colX, colWidth float32) {
	if len(events) == 0 {
		return
	}

	groups := overlapGroups(events)
	for _, group := range groups {
		fracs := bspAssign(group)
		for i, e := range group {
			lr := fracs[i]
			startFrac := timeOfDayFraction(time.Unix(e.StartTS, 0))
			var endFrac float32
			if e.EndTS.Valid {
				endFrac = timeOfDayFraction(time.Unix(e.EndTS.Int64, 0))
			} else {
				endFrac = startFrac + 1.0/24.0
			}
			if endFrac <= startFrac {
				endFrac = startFrac + 1.0/24.0
			}

			eX := colX + lr[0]*colWidth + 1
			eY := startFrac * hourHeight * 24
			eW := (lr[1]-lr[0])*colWidth - 2
			eH := (endFrac - startFrac) * hourHeight * 24
			if eH < 14 {
				eH = 14
			}

			col := parseHexColor(r.grid.calendarColor(e))
			rect := canvas.NewRectangle(col)
			rect.Move(fyne.NewPos(eX, eY))
			rect.Resize(fyne.NewSize(eW, eH))
			r.objects = append(r.objects, rect)

			isNarrow := eW < popOutWidth
			if eH >= 14 {
				label := "…"
				if !isNarrow {
					label = truncate(e.Title, 20)
				}
				title := canvas.NewText(label, eventTextCol)
				title.TextSize = 11
				title.Move(fyne.NewPos(eX+3, eY+2))
				r.objects = append(r.objects, title)
			}

			// Category color dots — up to 4, rendered in the bottom-left corner.
			if len(e.Categories) > 0 && eH >= 22 {
				dotY := eY + eH - 8
				for di, cat := range e.Categories {
					if di >= 4 {
						break
					}
					dot := canvas.NewRectangle(parseHexColor(cat.Color))
					dot.Move(fyne.NewPos(eX+3+float32(di)*8, dotY))
					dot.Resize(fyne.NewSize(6, 6))
					r.objects = append(r.objects, dot)
				}
			}

			var groupEvt []api.Event
			if isNarrow {
				groupEvt = group
			}
			r.grid.eventRects = append(r.grid.eventRects, eventRect{
				event: e, groupEvt: groupEvt, x: eX, y: eY, w: eW, h: eH,
			})
		}
	}
}

func (g *TimeGridWidget) calendarColor(e api.Event) string {
	if e.CalendarID.Valid {
		if cal, ok := g.calendars[e.CalendarID.Int64]; ok {
			return cal.Color
		}
	}
	return "#3B82F6"
}

// ---- BSP layout ----

// bspAssign returns fracs[i] = [leftFrac, rightFrac] for events[i] within [0,1].
// Uses slice indices rather than event IDs to avoid collisions with virtual occurrences (ID=0).
func bspAssign(events []api.Event) [][2]float32 {
	type block struct {
		l, r float32
		idx  int
	}
	n := len(events)
	if n == 0 {
		return nil
	}

	blocks := []block{{l: 0, r: 1, idx: 0}}

	for i := 1; i < n; i++ {
		widestIdx := 0
		widest := blocks[0].r - blocks[0].l
		for j, b := range blocks[1:] {
			if w := b.r - b.l; w > widest {
				widest = w
				widestIdx = j + 1
			}
		}
		b := blocks[widestIdx]
		mid := (b.l + b.r) / 2
		blocks[widestIdx] = block{l: b.l, r: mid, idx: b.idx}
		blocks = append(blocks, block{l: mid, r: b.r, idx: i})
	}

	result := make([][2]float32, n)
	for _, b := range blocks {
		result[b.idx] = [2]float32{b.l, b.r}
	}
	return result
}

// ---- overlap grouping ----

func overlapGroups(events []api.Event) [][]api.Event {
	n := len(events)
	used := make([]bool, n)
	var groups [][]api.Event

	for i := range n {
		if used[i] {
			continue
		}
		idxs := []int{i}
		used[i] = true
		for changed := true; changed; {
			changed = false
			for j := range n {
				if used[j] {
					continue
				}
				for _, k := range idxs {
					if eventsOverlap(events[k], events[j]) {
						idxs = append(idxs, j)
						used[j] = true
						changed = true
						break
					}
				}
			}
		}
		g := make([]api.Event, len(idxs))
		for k, idx := range idxs {
			g[k] = events[idx]
		}
		groups = append(groups, g)
	}
	return groups
}

func eventsOverlap(a, b api.Event) bool {
	aEnd := effectiveEndTS(a)
	bEnd := effectiveEndTS(b)
	return a.StartTS < bEnd && b.StartTS < aEnd
}

func effectiveEndTS(e api.Event) int64 {
	if e.EndTS.Valid && e.EndTS.Int64 > e.StartTS {
		return e.EndTS.Int64
	}
	return e.StartTS + 3600
}

// ---- helpers ----

func eventsForDay(events []api.Event, day time.Time) []api.Event {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Unix()
	dayEnd := dayStart + 86400
	var result []api.Event
	for _, e := range events {
		if e.AllDay {
			continue
		}
		if e.StartTS >= dayStart && e.StartTS < dayEnd {
			if isCalendarVisible(calIDOf(e)) && isEventMatchingFilter(e) {
				result = append(result, e)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTS < result[j].StartTS
	})
	return result
}

func allDayEventsForDay(events []api.Event, day time.Time) []api.Event {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Unix()
	dayEnd := dayStart + 86400
	var result []api.Event
	for _, e := range events {
		if !e.AllDay {
			continue
		}
		if e.StartTS >= dayStart && e.StartTS < dayEnd {
			if isCalendarVisible(calIDOf(e)) && isEventMatchingFilter(e) {
				result = append(result, e)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTS < result[j].StartTS
	})
	return result
}

func calIDOf(e api.Event) int64 {
	if e.CalendarID.Valid {
		return e.CalendarID.Int64
	}
	return 1
}

func timeOfDayFraction(t time.Time) float32 {
	sod := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	elapsed := t.Sub(sod)
	return float32(elapsed.Hours()) / 24.0
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func parseHexColor(hex string) color.RGBA {
	c := color.RGBA{A: 255}
	if len(hex) == 7 && hex[0] == '#' {
		fmt.Sscanf(hex[1:], "%02x%02x%02x", &c.R, &c.G, &c.B)
	}
	return c
}
