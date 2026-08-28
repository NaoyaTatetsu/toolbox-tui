package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	gh "github.com/NaoyaTatetsu/task-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/task-tui/internal/google-calendar"
)

const (
	calendarWeeks = 6
	minCellWidth  = 4
	maxCellWidth  = 18
	minAgendaCols = 26
)

type calendarState struct {
	day    time.Time // the selected day, at local midnight
	agenda int       // cursor within the selected day's agenda
}

func newCalendarState(now time.Time) calendarState {
	return calendarState{day: startOfDay(now)}
}

func (m Model) updateCalendar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := &m.month
	switch msg.String() {
	case "left":
		c.day = c.day.AddDate(0, 0, -1)
		c.agenda = 0
	case "right":
		c.day = c.day.AddDate(0, 0, 1)
		c.agenda = 0
	case "up":
		c.day = c.day.AddDate(0, 0, -7)
		c.agenda = 0
	case "down":
		c.day = c.day.AddDate(0, 0, 7)
		c.agenda = 0
	case "H", "pgup":
		c.day = c.day.AddDate(0, -1, 0)
		c.agenda = 0
	case "L", "pgdown":
		c.day = c.day.AddDate(0, 1, 0)
		c.agenda = 0
	case "t":
		c.day = startOfDay(m.now)
		c.agenda = 0
	case "J", "shift+down":
		c.agenda++
	case "K", "shift+up":
		if c.agenda > 0 {
			c.agenda--
		}
	case "enter":
		if it, ok := m.agendaTask(); ok {
			m.detail = detailState{item: it}
			m.overlay = overlayDetail
		}
	case "o":
		if it, ok := m.agendaTask(); ok {
			return m, openURL(it.URL)
		}
	}
	return m, nil
}

// agendaEntry is one line of the day pane: either a calendar event or a task.
// A task is "spanning" when the day merely falls inside its start–end range
// rather than being the day it is due.
type agendaEntry struct {
	event    *googlecalendar.Event
	task     *gh.Item
	spanning bool
}

// agenda collects everything happening on the selected day: calendar events
// first, then tasks due that day, then tasks merely in flight across it.
func (m Model) agenda(day time.Time) []agendaEntry {
	var out, spanning []agendaEntry
	for i := range m.events {
		if m.events[i].OccursOn(day) {
			out = append(out, agendaEntry{event: &m.events[i]})
		}
	}
	if m.project != nil {
		for i := range m.project.Items {
			it := &m.project.Items[i]
			if it.StartDate == nil && it.EndDate == nil {
				continue
			}
			if it.EndDate != nil && sameDay(*it.EndDate, day) {
				out = append(out, agendaEntry{task: it})
				continue
			}
			s, e := it.Span()
			if !startOfDay(day).Before(startOfDay(s)) && !startOfDay(day).After(startOfDay(e)) {
				spanning = append(spanning, agendaEntry{task: it, spanning: true})
			}
		}
	}
	return append(out, spanning...)
}

// dayCellEntries is what the month grid shows. Long-running tasks are left out:
// repeating a task on every day of its span buries the days that matter, and
// the roadmap already draws spans properly.
func (m Model) dayCellEntries(day time.Time) []agendaEntry {
	all := m.agenda(day)
	out := all[:0:0]
	for _, e := range all {
		if !e.spanning {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) agendaTask() (gh.Item, bool) {
	entries := m.agenda(m.month.day)
	if m.month.agenda < 0 || m.month.agenda >= len(entries) {
		return gh.Item{}, false
	}
	if t := entries[m.month.agenda].task; t != nil {
		return *t, true
	}
	return gh.Item{}, false
}

func (m Model) renderCalendar(width, height, top int) string {
	if len(m.cfg.Calendar.Sources) == 0 && m.project == nil {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			styMuted.Render("no calendar sources configured — add [[calendar.sources]] to "+m.cfg.ConfigPath()))
	}

	// Side by side when there is room, stacked otherwise.
	agendaW := 0
	gridW := width
	if width >= 7*minCellWidth+minAgendaCols+1 {
		agendaW = clamp(width/3, minAgendaCols, 44)
		gridW = width - agendaW - 1
	}
	cellW := clamp(gridW/7, minCellWidth, maxCellWidth)
	gridW = cellW * 7

	gridHeight := height
	agendaHeight := height
	if agendaW == 0 {
		gridHeight = max(calendarWeeks+3, height/2)
		agendaHeight = height - gridHeight
	}

	paneW := agendaW
	if paneW == 0 {
		paneW = gridW // stacked layout: the agenda gets the full width
	}
	// The agenda sits to the right when there is room, and below when there is
	// not; its origin differs accordingly.
	agendaX, agendaY := gridW+1, top
	if agendaW == 0 {
		agendaX, agendaY = 0, top+gridHeight+1
	}
	grid := m.renderMonthGrid(gridW, gridHeight, cellW, top)
	pane := m.renderAgenda(paneW, agendaHeight, agendaX, agendaY)

	if agendaW == 0 {
		return grid + "\n" + pane
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(gridW+1).Render(grid),
		lipgloss.NewStyle().Width(agendaW).Render(pane))
}

func (m Model) renderMonthGrid(width, height, cellW, top int) string {
	sel := m.month.day
	first := time.Date(sel.Year(), sel.Month(), 1, 0, 0, 0, 0, sel.Location())
	gridStart := first.AddDate(0, 0, -int(first.Weekday())) // back to Sunday

	// Rows shrink before the grid does, so the whole month always fits.
	rowH := clamp((height-2)/calendarWeeks, 1, 6)

	var b strings.Builder
	b.WriteString(styTitle.Render(center(sel.Format("January 2006"), width)) + "\n")

	var head strings.Builder
	for i := 0; i < 7; i++ {
		name := time.Weekday(i).String()[:min(3, cellW)]
		s := styMuted
		if i == 0 || i == 6 {
			s = lipgloss.NewStyle().Foreground(colDanger).Faint(true)
		}
		head.WriteString(s.Render(center(name, cellW)))
	}
	b.WriteString(head.String() + "\n")

	// Two header lines precede the weeks: the month name and the weekday row.
	weeksTop := top + 2
	for w := 0; w < calendarWeeks; w++ {
		lines := make([]strings.Builder, rowH)
		for d := 0; d < 7; d++ {
			day := gridStart.AddDate(0, 0, w*7+d)
			for li, cell := range m.renderDayCell(day, sel, cellW, rowH) {
				if li < rowH {
					lines[li].WriteString(cell)
				}
			}
			m.hits.add(hitRegion{
				x: d * cellW, y: weeksTop + w*rowH, w: cellW, h: rowH,
				kind: hitCalendarDay, day: day,
			})
		}
		for _, l := range lines {
			b.WriteString(l.String() + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderDayCell returns exactly rowH lines for one day of the month grid.
func (m Model) renderDayCell(day, sel time.Time, cellW, rowH int) []string {
	inMonth := day.Month() == sel.Month()
	isToday := sameDay(day, m.now)
	isSel := sameDay(day, sel)

	base := lipgloss.NewStyle()
	switch {
	case !inMonth:
		base = base.Foreground(colMuted).Faint(true)
	case day.Weekday() == time.Sunday || day.Weekday() == time.Saturday:
		base = base.Foreground(colDanger)
	default:
		base = base.Foreground(colFg)
	}
	if isToday {
		base = base.Background(colTodayBG).Bold(true)
	}
	if isSel {
		base = base.Background(colSelBG).Bold(true)
	}

	entries := m.dayCellEntries(day)
	lines := make([]string, rowH)

	num := fmt.Sprintf("%d", day.Day())
	if isSel {
		num = "[" + num + "]"
	}
	lines[0] = base.Render(pad(" "+num, cellW))

	if rowH == 1 {
		// One-line cells: append a density marker beside the day number.
		marker := densityMarker(entries)
		lines[0] = base.Render(pad(" "+num+" "+marker, cellW))
		return lines
	}

	for i := 1; i < rowH; i++ {
		idx := i - 1
		if idx >= len(entries) {
			lines[i] = base.Render(strings.Repeat(" ", cellW))
			continue
		}
		// The last line summarises the remainder instead of showing one more.
		if i == rowH-1 && len(entries) > rowH-1 {
			lines[i] = base.Foreground(colMuted).Render(pad(fmt.Sprintf(" +%d more", len(entries)-idx), cellW))
			continue
		}
		lines[i] = base.Render(pad(" "+entrySummary(entries[idx], cellW-1), cellW))
	}
	return lines
}

// densityMarker compresses a day's contents into a couple of glyphs for the
// narrowest layout.
func densityMarker(entries []agendaEntry) string {
	var ev, task int
	for _, e := range entries {
		if e.event != nil {
			ev++
		} else {
			task++
		}
	}
	s := ""
	if ev > 0 {
		s += "●"
	}
	if task > 0 {
		s += "▪"
	}
	return s
}

func entrySummary(e agendaEntry, width int) string {
	if e.event != nil {
		prefix := ""
		if !e.event.AllDay {
			prefix = e.event.Start.Format("15:04") + " "
		}
		return truncate(prefix+e.event.Summary, width)
	}
	return truncate("▪ "+e.task.Title, width)
}

func (m Model) renderAgenda(width, height, originX, originY int) string {
	day := m.month.day
	entries := m.agenda(day)

	var b strings.Builder
	title := day.Format("2006-01-02 (Mon)")
	if sameDay(day, m.now) {
		title += "  today"
	}
	b.WriteString(styTitle.Render(truncate(title, width)) + "\n")
	b.WriteString(styMuted.Render(strings.Repeat("─", max(1, width-1))) + "\n")

	if len(entries) == 0 {
		b.WriteString(styMuted.Render("nothing scheduled"))
		return b.String()
	}

	cursor := clamp(m.month.agenda, 0, len(entries)-1)
	rows := max(1, height-3)
	start := clamp(cursor-rows/2, 0, max(0, len(entries)-rows))

	// The title and rule occupy the first two lines of the pane.
	lineY := originY + 2
	for i := start; i < len(entries) && i < start+rows; i++ {
		e := entries[i]
		entryTop := lineY
		marker := "  "
		if i == cursor {
			marker = styAccent.Render("▸ ")
		}
		if e.event != nil {
			ev := e.event
			timeCol := stySubtle.Render(pad(ev.TimeLabel(), 12))
			line := marker + timeCol + truncate(ev.Summary, max(4, width-15))
			b.WriteString(line + "\n")
			lineY++
			if ev.Location != "" && width > 34 {
				b.WriteString(styMuted.Render("    "+truncate(ev.Location, width-6)) + "\n")
				lineY++
			}
			m.hits.add(hitRegion{
				x: originX, y: entryTop, w: width, h: lineY - entryTop,
				kind: hitAgendaEntry, index: i,
			})
			continue
		}
		it := e.task
		dot := lipgloss.NewStyle().Foreground(priorityColor(it.Priority)).Render("▪")
		label := it.Title
		if it.Number > 0 {
			label = fmt.Sprintf("#%d %s", it.Number, it.Title)
		}
		s := lipgloss.NewStyle().Foreground(colFg)
		if it.IsDone() {
			s = s.Faint(true).Strikethrough(true)
		} else if it.Overdue(m.now) {
			s = styDanger
		}
		suffix := " (due)"
		if e.spanning {
			suffix = " (wip)"
		}
		b.WriteString(marker + dot + " " +
			s.Render(truncate(label, max(4, width-8-len(suffix)))) +
			styMuted.Render(suffix) + "\n")
		lineY++
		m.hits.add(hitRegion{
			x: originX, y: entryTop, w: width, h: 1,
			kind: hitAgendaEntry, index: i,
		})
	}

	if len(entries) > rows {
		b.WriteString(styMuted.Render(fmt.Sprintf("%d/%d  (J/K to scroll)", cursor+1, len(entries))))
	} else {
		b.WriteString(styMuted.Render(keyHint([2]string{"J/K", "select"}, [2]string{"enter", "task detail"})))
	}
	return b.String()
}
