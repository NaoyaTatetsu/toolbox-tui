package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	gh "github.com/NaoyaTatetsu/toolbox-tui/internal/github"
)

// zoomWindows are the timeline widths, in days, that [ and ] cycle through.
var zoomWindows = []int{14, 28, 56, 112}

const (
	roadmapHeaderRows = 2
	minLabelWidth     = 16
	maxLabelWidth     = 34
)

type roadmapState struct {
	items   []gh.Item
	cursor  int
	rowOff  int
	origin  time.Time // first day shown, at local midnight
	zoom    int
	dayDflt int
}

func newRoadmapState(now time.Time, defaultDays int) roadmapState {
	zoom := 1
	for i, d := range zoomWindows {
		if d >= defaultDays {
			zoom = i
			break
		}
	}
	return roadmapState{
		origin:  startOfDay(now).AddDate(0, 0, -3),
		zoom:    zoom,
		dayDflt: defaultDays,
	}
}

// setItems installs the scheduled tasks, keeping the cursor on the same task
// across a reload. On the first load it lands on the earliest task that has not
// finished yet, so the view opens on live work instead of on a wall of arrows
// pointing at last month.
func (r *roadmapState) setItems(items []gh.Item, now time.Time) {
	prev := ""
	if r.cursor >= 0 && r.cursor < len(r.items) {
		prev = r.items[r.cursor].ID
	}
	first := len(r.items) == 0
	r.items = items

	if prev != "" {
		for i, it := range items {
			if it.ID == prev {
				r.cursor = i
				return
			}
		}
	}
	if first {
		today := startOfDay(now)
		for i, it := range items {
			if _, end := it.Span(); !startOfDay(end).Before(today) {
				r.cursor = i
				return
			}
		}
	}
	r.cursor = clamp(r.cursor, 0, max(0, len(items)-1))
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// daysBetween counts calendar days from a to b, ignoring clock time and DST.
func daysBetween(a, b time.Time) int {
	return int(startOfDay(b).Sub(startOfDay(a)).Hours() / 24 * 1.0000001)
}

func (m Model) updateRoadmap(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := &m.roadmap
	step := max(1, zoomWindows[r.zoom]/7)
	switch msg.String() {
	case "down":
		if r.cursor < len(r.items)-1 {
			r.cursor++
		}
	case "up":
		if r.cursor > 0 {
			r.cursor--
		}
	case "home":
		r.cursor = 0
	case "end":
		r.cursor = max(0, len(r.items)-1)
	case "left":
		r.origin = r.origin.AddDate(0, 0, -step)
	case "right":
		r.origin = r.origin.AddDate(0, 0, step)
	case "-", "_":
		// Zoom moved off [ and ] when those became the global view switch.
		if r.zoom < len(zoomWindows)-1 {
			r.zoom++
		}
	case "+", "=":
		if r.zoom > 0 {
			r.zoom--
		}
	case "t":
		r.origin = startOfDay(m.now).AddDate(0, 0, -3)
	case "f":
		// Frame the selected task: scroll the window to its start.
		if r.cursor < len(r.items) {
			s, _ := r.items[r.cursor].Span()
			r.origin = startOfDay(s).AddDate(0, 0, -2)
		}
	case "enter":
		if r.cursor < len(r.items) {
			m.detail = detailState{item: r.items[r.cursor]}
			m.overlay = overlayDetail
		}
	case "o":
		if r.cursor < len(r.items) {
			return m, openURL(r.items[r.cursor].URL)
		}
	}
	return m, nil
}

func (m Model) renderRoadmap(width, height, top int) string {
	r := m.roadmap
	if len(r.items) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			styMuted.Render("no tasks have a Start Date or End Date"))
	}

	labelW := clamp(width/3, minLabelWidth, maxLabelWidth)
	timelineW := width - labelW - 1
	if timelineW < 8 {
		labelW = max(8, width-9)
		timelineW = width - labelW - 1
	}
	cellW := max(1, timelineW/zoomWindows[r.zoom])
	days := max(1, timelineW/cellW)

	rows := max(1, height-roadmapHeaderRows-1)
	rowOff := clamp(r.rowOff, 0, max(0, len(r.items)-rows))
	if r.cursor < rowOff {
		rowOff = r.cursor
	}
	if r.cursor >= rowOff+rows {
		rowOff = r.cursor - rows + 1
	}

	var b strings.Builder
	b.WriteString(m.roadmapHeader(labelW, days, cellW))

	todayIdx := daysBetween(r.origin, m.now)
	rowsTop := top + roadmapHeaderRows
	for i := rowOff; i < len(r.items) && i < rowOff+rows; i++ {
		b.WriteString(m.roadmapRow(r.items[i], i == r.cursor, labelW, days, cellW, todayIdx) + "\n")
		m.hits.add(hitRegion{
			x: 0, y: rowsTop + (i - rowOff), w: width, h: 1,
			kind: hitRoadmapRow, index: i,
		})
	}

	last := r.origin.AddDate(0, 0, days-1)
	legend := fmt.Sprintf("%s – %s  ·  %d days  ·  %d/%d tasks",
		r.origin.Format("2006-01-02"), last.Format("2006-01-02"), days, r.cursor+1, len(r.items))
	b.WriteString(styMuted.Render(legend))
	return b.String()
}

// roadmapHeader draws the month band and the day/week ruler.
func (m Model) roadmapHeader(labelW, days, cellW int) string {
	r := m.roadmap

	// Month band: write each month's name at the cell where it starts.
	band := []rune(strings.Repeat(" ", days*cellW))
	writeAt := func(pos int, s string) {
		for i, ch := range []rune(s) {
			if pos+i >= 0 && pos+i < len(band) {
				band[pos+i] = ch
			}
		}
	}
	for d := 0; d < days; d++ {
		day := r.origin.AddDate(0, 0, d)
		if d == 0 || day.Day() == 1 {
			writeAt(d*cellW, day.Format("Jan 2006"))
		}
	}
	monthLine := pad("", labelW) + " " + styAccent.Render(string(band))

	// Ruler: day numbers when there is room, otherwise week-start ticks.
	var ruler strings.Builder
	todayIdx := daysBetween(r.origin, m.now)
	for d := 0; d < days; d++ {
		day := r.origin.AddDate(0, 0, d)
		var cell string
		switch {
		case cellW >= 3:
			cell = center(fmt.Sprintf("%d", day.Day()), cellW)
		case cellW == 2:
			cell = fmt.Sprintf("%2d", day.Day()%100)
		case day.Weekday() == time.Monday:
			cell = "┊"
		default:
			cell = "·"
		}
		style := styMuted
		switch {
		case d == todayIdx:
			style = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
		case day.Weekday() == time.Saturday || day.Weekday() == time.Sunday:
			style = lipgloss.NewStyle().Foreground(colDanger).Faint(true)
		}
		ruler.WriteString(style.Render(cell))
	}
	rulerLine := styMuted.Render(pad("Task", labelW)) + " " + ruler.String()

	return monthLine + "\n" + rulerLine + "\n"
}

// roadmapRow draws one task: its label on the left and its bar on the timeline.
func (m Model) roadmapRow(it gh.Item, selected bool, labelW, days, cellW, todayIdx int) string {
	dot := lipgloss.NewStyle().Foreground(priorityColor(it.Priority)).Render("●")
	title := it.Title
	if it.Number > 0 {
		title = fmt.Sprintf("#%d %s", it.Number, it.Title)
	}
	labelStyle := lipgloss.NewStyle().Foreground(colFg)
	if it.IsDone() {
		labelStyle = labelStyle.Faint(true)
	}
	if selected {
		labelStyle = labelStyle.Bold(true).Foreground(colAccent)
	}
	label := dot + labelStyle.Render(pad(truncate(title, labelW-1), labelW-1))

	start, end := it.Span()
	from := daysBetween(m.roadmap.origin, start)
	to := daysBetween(m.roadmap.origin, end)

	barColor := ghColor(m.statusColor(it.Status))
	if it.Overdue(m.now) {
		barColor = colDanger
	} else if it.IsDone() {
		barColor = colMuted
	}
	barStyle := lipgloss.NewStyle().Foreground(barColor)
	if selected {
		barStyle = barStyle.Background(colSelBG)
	}

	cells := make([]string, days)
	for d := 0; d < days; d++ {
		switch {
		case d >= from && d <= to:
			cells[d] = barStyle.Render(strings.Repeat("█", cellW))
		case d == todayIdx:
			cells[d] = lipgloss.NewStyle().Foreground(colAccent).Render(center("│", cellW))
		case selected:
			cells[d] = lipgloss.NewStyle().Background(colSelBG).Render(strings.Repeat(" ", cellW))
		default:
			cells[d] = strings.Repeat(" ", cellW)
		}
	}

	// Arrows mark a task whose bar lies entirely outside the current window, so
	// it stays findable instead of rendering as a blank row.
	arrow := lipgloss.NewStyle().Foreground(barColor).Bold(true)
	if to < 0 {
		cells[0] = arrow.Render(center("◀", cellW))
	} else if from >= days {
		cells[days-1] = arrow.Render(center("▶", cellW))
	}

	return label + " " + strings.Join(cells, "")
}

// statusColor looks up the project's palette colour for a status name.
func (m Model) statusColor(status string) string {
	if m.project == nil {
		return "BLUE"
	}
	f, ok := m.project.Field(gh.FieldStatus)
	if !ok {
		return "BLUE"
	}
	if o, ok := f.OptionByName(status); ok {
		return o.Color
	}
	return "GRAY"
}
