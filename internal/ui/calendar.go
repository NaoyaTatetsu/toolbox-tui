package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	googlecalendar "github.com/NaoyaTatetsu/toolbox-tui/internal/google-calendar"
)

const (
	calendarWeeks = 6
	minCellWidth  = 4
	maxCellWidth  = 18
	minAgendaCols = 26
	// Below this, a day cell cannot hold a two-digit date beside its column
	// rule with anything left over, so the grid keeps the whole width and the
	// agenda moves underneath it instead.
	minSplitCell = 6
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
	}
	return m, nil
}

// agenda collects the events happening on the selected day. Tasks are
// deliberately absent: the board and the roadmap own them, and folding due
// dates into the calendar left both the month cells and the day pane too busy
// to read at a glance.
func (m Model) agenda(day time.Time) []googlecalendar.Event {
	var out []googlecalendar.Event
	for i := range m.events {
		if m.events[i].OccursOn(day) {
			out = append(out, m.events[i])
		}
	}
	return out
}

func (m Model) renderCalendar(width, height, top int) string {
	if len(m.cfg.Calendar.Sources) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			styMuted.Render("no calendar sources configured — add [[calendar.sources]] to "+m.cfg.ConfigPath()))
	}

	// Side by side when there is room, stacked otherwise.
	agendaW := 0
	gridW := width
	if width >= 7*minSplitCell+minAgendaCols+1 {
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

// weekdayColor tints the weekend columns the way a wall calendar does: Sunday
// red, Saturday blue.
func weekdayColor(d time.Weekday) lipgloss.TerminalColor {
	switch d {
	case time.Sunday:
		return colSunday
	case time.Saturday:
		return colSaturday
	}
	return colFg
}

// Each day cell opens with a vertical rule, so columns stay separated without
// spending width on gaps; the row rules use the matching intersection glyph.
const (
	cellRule = "│"
	rowCross = "┼"
	rowRule  = "─"
)

// gridRule draws the horizontal rule between two week rows, aligned with the
// per-cell vertical rules.
func gridRule(cellW int) string {
	col := rowCross + strings.Repeat(rowRule, cellW-1)
	return styMuted.Render(strings.Repeat(col, 7))
}

func (m Model) renderMonthGrid(width, height, cellW, top int) string {
	sel := m.month.day
	first := time.Date(sel.Year(), sel.Month(), 1, 0, 0, 0, 0, sel.Location())
	gridStart := first.AddDate(0, 0, -int(first.Weekday())) // back to Sunday

	// Rows shrink before the grid does, so the whole month always fits. The
	// horizontal rules cost one line per week plus one under the weekday names;
	// a short pane spends those lines on the cells instead.
	rowH := clamp((height-2)/calendarWeeks, 1, 6)
	rules := (height-2-calendarWeeks)/calendarWeeks >= 1
	if rules {
		rowH = clamp((height-2-calendarWeeks)/calendarWeeks, 1, 6)
	}

	var b strings.Builder
	b.WriteString(styTitle.Render(center(sel.Format("January 2006"), width)) + "\n")

	sep := styMuted.Render(cellRule)
	var head strings.Builder
	for i := 0; i < 7; i++ {
		name := time.Weekday(i).String()[:min(3, cellW-1)]
		s := styMuted
		if i == 0 || i == 6 {
			s = lipgloss.NewStyle().Foreground(weekdayColor(time.Weekday(i))).Faint(true)
		}
		head.WriteString(sep + s.Render(center(name, cellW-1)))
	}
	b.WriteString(head.String() + "\n")

	// The month name and the weekday row precede the weeks, plus the rule under
	// the weekday row when there is room for it.
	weeksTop := top + 2
	if rules {
		b.WriteString(gridRule(cellW) + "\n")
		weeksTop++
	}
	rowStride := rowH
	if rules {
		rowStride++
	}
	for w := 0; w < calendarWeeks; w++ {
		if rules && w > 0 {
			b.WriteString(gridRule(cellW) + "\n")
		}
		lines := make([]strings.Builder, rowH)
		for d := 0; d < 7; d++ {
			day := gridStart.AddDate(0, 0, w*7+d)
			for li, cell := range m.renderDayCell(day, sel, cellW, rowH) {
				if li < rowH {
					lines[li].WriteString(cell)
				}
			}
			// The rule belongs to the column but not to the day: clicking it
			// would be ambiguous, so the hit region covers the text only.
			m.hits.add(hitRegion{
				x: d*cellW + 1, y: weeksTop + w*rowStride, w: cellW - 1, h: rowH,
				kind: hitCalendarDay, day: day,
			})
		}
		for _, l := range lines {
			b.WriteString(l.String() + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderDayCell returns exactly rowH lines for one day of the month grid. Each
// line opens with the column rule, leaving cellW-1 cells for the text.
func (m Model) renderDayCell(day, sel time.Time, cellW, rowH int) []string {
	inMonth := day.Month() == sel.Month()
	isToday := sameDay(day, m.now)
	isSel := sameDay(day, sel)
	textW := cellW - 1

	base := lipgloss.NewStyle().Foreground(weekdayColor(day.Weekday()))
	if !inMonth {
		base = base.Foreground(colMuted).Faint(true)
	}
	if isToday {
		base = base.Background(colTodayBG).Bold(true)
	}
	if isSel {
		base = base.Background(colSelBG).Bold(true)
	}

	events := m.agenda(day)
	sep := styMuted.Render(cellRule)
	lines := make([]string, rowH)

	num := fmt.Sprintf("%d", day.Day())
	// The brackets are how a monochrome terminal shows the selection; they are
	// only worth their two cells once the cell is wide enough to spare them.
	if isSel && textW >= 5 {
		num = "[" + num + "]"
	}
	lines[0] = sep + base.Render(pad(" "+num, textW))

	if rowH == 1 {
		// One-line cells: mark the day as busy beside its number, but never at
		// the cost of the number itself.
		line := " " + num
		if mk := densityMarker(events); mk != "" && textW >= len(line)+2 {
			line += " " + mk
		}
		lines[0] = sep + base.Render(pad(line, textW))
		return lines
	}

	for i := 1; i < rowH; i++ {
		idx := i - 1
		if idx >= len(events) {
			lines[i] = sep + base.Render(strings.Repeat(" ", textW))
			continue
		}
		// The last line summarises the remainder instead of showing one more.
		if i == rowH-1 && len(events) > rowH-1 {
			lines[i] = sep + base.Foreground(colMuted).Render(pad(fmt.Sprintf(" +%d more", len(events)-idx), textW))
			continue
		}
		lines[i] = sep + base.Render(pad(" "+eventSummary(events[idx], textW-1), textW))
	}
	return lines
}

// densityMarker compresses a day's contents into a glyph for the narrowest
// layout, where there is no room to name anything.
func densityMarker(events []googlecalendar.Event) string {
	if len(events) == 0 {
		return ""
	}
	return "●"
}

func eventSummary(e googlecalendar.Event, width int) string {
	prefix := ""
	if !e.AllDay {
		prefix = e.Start.Format("15:04") + " "
	}
	return truncate(prefix+e.Summary, width)
}

func (m Model) renderAgenda(width, height, originX, originY int) string {
	day := m.month.day
	events := m.agenda(day)

	var b strings.Builder
	title := day.Format("2006-01-02 (Mon)")
	if sameDay(day, m.now) {
		title += "  today"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(weekdayColor(day.Weekday())).
		Render(truncate(title, width)) + "\n")
	b.WriteString(styMuted.Render(strings.Repeat("─", max(1, width-1))) + "\n")

	if len(events) == 0 {
		b.WriteString(styMuted.Render("nothing scheduled"))
		return b.String()
	}

	cursor := clamp(m.month.agenda, 0, len(events)-1)
	rows := max(1, height-3)
	start := clamp(cursor-rows/2, 0, max(0, len(events)-rows))

	// The title and rule occupy the first two lines of the pane.
	lineY := originY + 2
	for i := start; i < len(events) && i < start+rows; i++ {
		ev := events[i]
		entryTop := lineY
		marker := "  "
		if i == cursor {
			marker = styAccent.Render("▸ ")
		}
		timeCol := stySubtle.Render(pad(ev.TimeLabel(), 12))
		b.WriteString(marker + timeCol + truncate(ev.Summary, max(4, width-15)) + "\n")
		lineY++
		if ev.Location != "" && width > 34 {
			b.WriteString(styMuted.Render("    "+truncate(ev.Location, width-6)) + "\n")
			lineY++
		}
		m.hits.add(hitRegion{
			x: originX, y: entryTop, w: width, h: lineY - entryTop,
			kind: hitAgendaEntry, index: i,
		})
	}

	if len(events) > rows {
		b.WriteString(styMuted.Render(fmt.Sprintf("%d/%d  (J/K to scroll)", cursor+1, len(events))))
	} else {
		b.WriteString(styMuted.Render(keyHint([2]string{"J/K", "select"})))
	}
	return b.String()
}
