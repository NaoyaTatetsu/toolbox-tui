package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	gh "github.com/NaoyaTatetsu/toolbox-tui/internal/github"
)

// cardHeight is fixed so that scrolling maths stays simple and columns line up
// even when some cards have no labels.
const (
	cardHeight   = 5 // 3 content lines + top/bottom border
	minColWidth  = 22
	maxColWidth  = 40
	colHeaderRow = 2
)

type boardState struct {
	cols      []gh.Column
	col       int
	row       int
	colOffset int
	rowOffset int
}

// setColumns installs freshly loaded columns, keeping the cursor on the same
// task when it is still on the board.
func (b *boardState) setColumns(cols []gh.Column) {
	prev := b.selectedID()
	b.cols = cols
	if prev != "" {
		for ci, c := range cols {
			for ri, it := range c.Items {
				if it.ID == prev {
					b.col, b.row = ci, ri
					b.clamp()
					return
				}
			}
		}
	}
	b.clamp()
}

func (b *boardState) clamp() {
	if len(b.cols) == 0 {
		b.col, b.row = 0, 0
		return
	}
	b.col = clamp(b.col, 0, len(b.cols)-1)
	n := len(b.cols[b.col].Items)
	if n == 0 {
		b.row = 0
	} else {
		b.row = clamp(b.row, 0, n-1)
	}
}

func (b boardState) selected() (gh.Item, bool) {
	if b.col < 0 || b.col >= len(b.cols) {
		return gh.Item{}, false
	}
	items := b.cols[b.col].Items
	if b.row < 0 || b.row >= len(items) {
		return gh.Item{}, false
	}
	return items[b.row], true
}

func (b boardState) selectedID() string {
	if it, ok := b.selected(); ok {
		return it.ID
	}
	return ""
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m Model) updateBoard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &m.board
	if len(b.cols) == 0 {
		return m, nil
	}
	switch msg.String() {
	case "left":
		if b.col > 0 {
			b.col--
			b.rowOffset = 0
			b.clamp()
		}
	case "right":
		if b.col < len(b.cols)-1 {
			b.col++
			b.rowOffset = 0
			b.clamp()
		}
	case "down":
		if b.row < len(b.cols[b.col].Items)-1 {
			b.row++
		}
	case "up":
		if b.row > 0 {
			b.row--
		}
	case "home":
		b.row = 0
	case "end":
		b.row = max(0, len(b.cols[b.col].Items)-1)
	case "H":
		return m.moveSelected(-1)
	case "L":
		return m.moveSelected(+1)
	case "enter":
		if it, ok := b.selected(); ok {
			m.detail = detailState{item: it}
			m.overlay = overlayDetail
		}
	case "o":
		if it, ok := b.selected(); ok {
			return m, openURL(it.URL)
		}
	}
	return m, nil
}

// moveSelected shifts the selected card one column left or right, updating the
// Status field on GitHub. The board is updated optimistically so the card moves
// under the cursor immediately; a failed mutation surfaces in the status line
// and the next refresh restores the truth.
func (m Model) moveSelected(dir int) (tea.Model, tea.Cmd) {
	b := &m.board
	item, ok := b.selected()
	if !ok || m.project == nil {
		return m, nil
	}
	target := b.col + dir
	if target < 0 || target >= len(b.cols) {
		return m, nil
	}
	dest := b.cols[target]
	if dest.Name == "No Status" {
		m.status = "cannot move into No Status"
		m.statusOK = false
		return m, clearStatusAfter(3 * time.Second)
	}

	// Optimistic local move.
	src := &b.cols[b.col]
	src.Items = append(src.Items[:b.row], src.Items[b.row+1:]...)
	item.Status = dest.Name
	b.cols[target].Items = append(b.cols[target].Items, item)
	gh.SortItems(b.cols[target].Items, m.now)

	b.col = target
	b.row = 0
	for i, it := range b.cols[target].Items {
		if it.ID == item.ID {
			b.row = i
			break
		}
	}
	b.rowOffset = 0
	b.clamp()

	m.status = "moving to " + dest.Name
	m.statusOK = false
	return m, m.moveStatus(item, dest.Name)
}

func (m Model) renderBoard(width, height, top int) string {
	b := m.board
	if len(b.cols) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			styMuted.Render("no columns — does this project have a Status field?"))
	}

	visible, colW := boardLayout(width, len(b.cols))
	offset := b.colOffset
	// Keep the selected column on screen.
	if b.col < offset {
		offset = b.col
	}
	if b.col >= offset+visible {
		offset = b.col - visible + 1
	}
	offset = clamp(offset, 0, max(0, len(b.cols)-visible))

	perCol := max(1, (height-colHeaderRow)/cardHeight)
	rowOffset := b.rowOffset
	if b.row < rowOffset {
		rowOffset = b.row
	}
	if b.row >= rowOffset+perCol {
		rowOffset = b.row - perCol + 1
	}

	var rendered []string
	for ci := offset; ci < len(b.cols) && ci < offset+visible; ci++ {
		col := b.cols[ci]
		sel := ci == b.col
		start := 0
		if sel {
			start = rowOffset
		}
		rendered = append(rendered, m.renderColumn(col, colW, perCol, start, sel, b.row))

		// Record where each visible card landed, for click selection.
		cardsTop := top + colHeaderRow
		for i, shown := start, 0; i < len(col.Items) && shown < perCol; i, shown = i+1, shown+1 {
			m.hits.add(hitRegion{
				x:    (ci - offset) * colW,
				y:    cardsTop + shown*cardHeight,
				w:    colW,
				h:    cardHeight,
				kind: hitBoardCard,
				col:  ci,
				row:  i,
			})
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	if len(b.cols) > visible {
		board += "\n" + styMuted.Render(fmt.Sprintf("  columns %d–%d of %d  (h/l to scroll)",
			offset+1, min(offset+visible, len(b.cols)), len(b.cols)))
	}
	return board
}

// boardLayout picks how many columns fit and how wide each one is.
func boardLayout(width, ncols int) (visible, colW int) {
	if width < minColWidth {
		return 1, max(8, width)
	}
	visible = min(ncols, max(1, width/minColWidth))
	colW = clamp(width/visible, minColWidth, maxColWidth)
	// Recompute how many actually fit once the width is clamped.
	visible = min(ncols, max(1, width/colW))
	return visible, colW
}

func (m Model) renderColumn(col gh.Column, width, perCol, start int, selected bool, selRow int) string {
	head := fmt.Sprintf("%s (%d)", col.Name, len(col.Items))
	headStyle := styColHead.Foreground(ghColor(col.Color))
	if !selected {
		headStyle = headStyle.Faint(true)
	}
	lines := []string{headStyle.Render(pad(head, width-1))}

	underline := "─"
	if selected {
		underline = "━"
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(ghColor(col.Color)).Render(strings.Repeat(underline, width-1)))

	shown := 0
	for i := start; i < len(col.Items) && shown < perCol; i++ {
		lines = append(lines, m.renderCard(col.Items[i], width, selected && i == selRow))
		shown++
	}
	if len(col.Items) == 0 {
		lines = append(lines, styMuted.Render(pad("  —", width-1)))
	}
	if more := len(col.Items) - start - shown; more > 0 {
		lines = append(lines, styMuted.Render(pad(fmt.Sprintf("  +%d more", more), width-1)))
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(width).Render(body)
}

// renderCard draws one task card at a fixed height so columns stay aligned.
func (m Model) renderCard(it gh.Item, width int, selected bool) string {
	style := styCard
	if selected {
		style = styCardSel
	}
	// The card is one cell narrower than its column, which is what creates the
	// gutter between lanes. Width() covers content plus padding, borders sit
	// outside it, so the text area is another 2 cells in.
	boxW := max(6, width-3)
	inner := max(4, boxW-2)

	title := it.Title
	if it.Number > 0 {
		title = fmt.Sprintf("#%d %s", it.Number, it.Title)
	}
	titleStyle := lipgloss.NewStyle().Foreground(colFg)
	if selected {
		titleStyle = titleStyle.Bold(true)
	}
	if it.IsDone() {
		titleStyle = titleStyle.Faint(true).Strikethrough(true)
	}
	line1 := titleStyle.Render(pad(truncate(title, inner), inner))

	// Line 2: priority dot + due date.
	var meta []string
	if it.Priority != "" {
		meta = append(meta, lipgloss.NewStyle().Foreground(priorityColor(it.Priority)).Render("●"+it.Priority))
	}
	if it.EndDate != nil {
		label := it.EndDate.Format("01/02")
		s := stySubtle
		switch {
		case it.Overdue(m.now):
			s = styDanger.Bold(true)
			label = "!" + label
		case sameDay(*it.EndDate, m.now):
			s = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
			label = "→" + label
		}
		meta = append(meta, s.Render(label))
	}
	if len(it.Assignees) > 0 {
		meta = append(meta, styMuted.Render("@"+it.Assignees[0]))
	}
	line2 := pad(truncate(strings.Join(meta, " "), inner), inner)
	if len(meta) == 0 {
		line2 = strings.Repeat(" ", inner)
	}

	// Line 3: label chips.
	line3 := strings.Repeat(" ", inner)
	if len(it.Labels) > 0 {
		var chips []string
		for _, l := range it.Labels {
			chips = append(chips, lipgloss.NewStyle().Foreground(hexColor(l.Color)).Render(l.Name))
		}
		line3 = pad(truncate(strings.Join(chips, " "), inner), inner)
	}

	return style.Width(boxW).Render(line1 + "\n" + line2 + "\n" + line3)
}

// ---- detail overlay ----

type detailState struct {
	item   gh.Item
	scroll int
}

func (m Model) renderDetail() string {
	it := m.detail.item
	w := clamp(m.width-10, 40, 90)
	inner := w - 6

	var b strings.Builder
	title := it.Title
	if it.Number > 0 {
		title = fmt.Sprintf("#%d  %s", it.Number, it.Title)
	}
	b.WriteString(styTitle.Render(truncate(title, inner)) + "\n")
	if it.Repo != "" {
		b.WriteString(styMuted.Render(it.Repo) + "\n")
	}
	b.WriteString(styMuted.Render(strings.Repeat("─", inner)) + "\n")

	field := func(label, value string, style lipgloss.Style) {
		if value == "" {
			return
		}
		b.WriteString(styFieldLbl.Render(label) + style.Render(truncate(value, inner-11)) + "\n")
	}
	field("Status", it.Status, styAccent)
	field("Priority", it.Priority, lipgloss.NewStyle().Foreground(priorityColor(it.Priority)))
	if it.StartDate != nil {
		field("Start", it.StartDate.Format("2006-01-02 (Mon)"), stySubtle)
	}
	if it.EndDate != nil {
		s := stySubtle
		if it.Overdue(m.now) {
			s = styDanger
		}
		field("Due", it.EndDate.Format("2006-01-02 (Mon)"), s)
	}
	if len(it.Labels) > 0 {
		var chips []string
		for _, l := range it.Labels {
			chips = append(chips, lipgloss.NewStyle().Foreground(hexColor(l.Color)).Render(l.Name))
		}
		b.WriteString(styFieldLbl.Render("Labels") + truncate(strings.Join(chips, " "), inner-11) + "\n")
	}
	field("Assignees", strings.Join(it.Assignees, ", "), stySubtle)
	field("Milestone", it.Milestone, stySubtle)
	field("State", it.State, stySubtle)
	for k, v := range it.Extra {
		field(k, v, stySubtle)
	}

	if body := strings.TrimSpace(it.Body); body != "" {
		b.WriteString(styMuted.Render(strings.Repeat("─", inner)) + "\n")
		maxLines := clamp(m.height-24, 3, 20)
		lines := wrapLines(body, inner)
		start := clamp(m.detail.scroll, 0, max(0, len(lines)-maxLines))
		for i := start; i < len(lines) && i < start+maxLines; i++ {
			b.WriteString(stySubtle.Render(lines[i]) + "\n")
		}
		if len(lines) > maxLines {
			b.WriteString(styMuted.Render(fmt.Sprintf("… %d/%d lines (↑/↓ to scroll)", min(start+maxLines, len(lines)), len(lines))) + "\n")
		}
	}

	b.WriteString(styMuted.Render(strings.Repeat("─", inner)) + "\n")
	if it.URL != "" {
		b.WriteString(styMuted.Render(truncate(it.URL, inner)) + "\n")
	}
	b.WriteString(keyHint([2]string{"↑↓", "scroll"}, [2]string{"o", "open in browser"}, [2]string{"esc", "close"}))

	return styOverlay.Width(w).Render(b.String())
}

// wrapLines hard-wraps text to width cells, preserving paragraph breaks.
func wrapLines(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, "\r")
		if para == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		for _, w := range words {
			candidate := w
			if line != "" {
				candidate = line + " " + w
			}
			if lipgloss.Width(candidate) > width && line != "" {
				out = append(out, line)
				line = w
				continue
			}
			line = candidate
		}
		// A single word longer than the pane still has to be cut somewhere.
		for lipgloss.Width(line) > width {
			out = append(out, truncate(line, width))
			line = ""
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (m Model) renderHelp() string {
	w := clamp(m.width-10, 44, 74)
	section := func(title string, rows [][2]string) string {
		var b strings.Builder
		b.WriteString(styAccent.Bold(true).Render(title) + "\n")
		for _, r := range rows {
			b.WriteString("  " + styKey.Render(pad(r[0], 12)) + stySubtle.Render(r[1]) + "\n")
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(styTitle.Render("toolbox-tui") + "\n\n")
	b.WriteString(section("Global", [][2]string{
		{"[ ]", "previous / next view"},
		{"1 2 3", "Board / Roadmap / Calendar"},
		{"n", "new task"},
		{", .", "scroll sensitivity, slower / faster"},
		{"r", "reload"},
		{"?", "this help"},
		{"q", "quit"},
	}))
	b.WriteString("\n")
	b.WriteString(section("Board", [][2]string{
		{"← →", "previous / next column"},
		{"↑ ↓", "previous / next card"},
		{"H L", "move card between statuses"},
		{"home end", "first / last card"},
		{"enter", "task detail"},
		{"o", "open on GitHub"},
	}))
	b.WriteString("\n")
	b.WriteString(section("Roadmap", [][2]string{
		{"↑ ↓", "select task"},
		{"← →", "scroll timeline"},
		{"- +", "zoom out / in"},
		{"t", "back to today"},
		{"f", "frame the selected task"},
		{"enter", "task detail"},
	}))
	b.WriteString("\n")
	b.WriteString(section("Calendar — month grid", [][2]string{
		{"← →", "previous / next day"},
		{"↑ ↓", "previous / next week"},
		{"H L", "previous / next month"},
		{"t", "back to today"},
		{"enter", "open the selected day"},
	}))
	b.WriteString("\n")
	b.WriteString(section("Calendar — day pane", [][2]string{
		{"↑ ↓", "previous / next entry (J K too)"},
		{"enter", "event detail"},
		{"esc ←", "back to the grid"},
	}))
	b.WriteString("\n" + styMuted.Render("press any key to close"))
	return styOverlay.Width(w).Render(b.String())
}
