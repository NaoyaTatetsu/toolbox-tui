package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	gh "github.com/NaoyaTatetsu/task-tui/internal/github"
)

// Form field order, as traversed by tab.
const (
	fldTitle = iota
	fldBody
	fldLabels
	fldStatus
	fldPriority
	fldStart
	fldEnd
	fldCount
)

type labelChoice struct {
	name  string
	color string
	on    bool
}

type formModel struct {
	ready bool
	width int
	focus int

	title textinput.Model
	body  textarea.Model
	start textinput.Model
	end   textinput.Model

	labels      []labelChoice
	labelCursor int

	statusOpts []gh.Option
	statusIdx  int
	prioOpts   []gh.Option
	prioIdx    int

	repoName string
	err      string
	busy     bool
}

// openForm prepares the registration form from the live project and repo
// metadata, so the pickers only ever offer values GitHub will accept.
func (m Model) openForm() (tea.Model, tea.Cmd) {
	if m.project == nil {
		m.status = "still loading the project"
		m.statusOK = false
		return m, clearStatusAfter(3 * time.Second)
	}
	if m.repo == nil {
		m.status = "set github.default_repo in " + m.cfg.ConfigPath() + " to register tasks"
		m.statusOK = false
		return m, clearStatusAfter(6 * time.Second)
	}

	f := formModel{ready: true, repoName: m.cfg.GitHub.DefaultRepo}

	f.title = textinput.New()
	f.title.Placeholder = "task title"
	f.title.CharLimit = 250
	f.title.Prompt = ""
	f.title.Focus()

	f.body = textarea.New()
	f.body.Placeholder = "description (optional)"
	f.body.SetHeight(4)
	f.body.ShowLineNumbers = false
	f.body.Prompt = ""

	f.start = textinput.New()
	f.start.Placeholder = "YYYY-MM-DD, today, +3d"
	f.start.Prompt = ""
	f.end = textinput.New()
	f.end.Placeholder = "YYYY-MM-DD, today, +3d"
	f.end.Prompt = ""

	for _, l := range m.repo.Labels {
		f.labels = append(f.labels, labelChoice{name: l.Name, color: l.Color})
	}

	if sf, ok := m.project.Field(gh.FieldStatus); ok {
		f.statusOpts = sf.Options
		// Default to the column the cursor is in, else the first non-Done status.
		if it, ok := m.board.selected(); ok && it.Status != "" {
			for i, o := range f.statusOpts {
				if strings.EqualFold(o.Name, it.Status) {
					f.statusIdx = i
				}
			}
		}
	}
	if pf, ok := m.project.Field(gh.FieldPriority); ok {
		f.prioOpts = pf.Options
		f.prioIdx = -1 // unset by default
	}

	f.setWidth(m.formWidth())
	m.form = f
	m.overlay = overlayForm
	return m, textinput.Blink
}

func (m Model) formWidth() int {
	return clamp(m.width-8, 40, 86)
}

func (f *formModel) setWidth(w int) {
	if !f.ready {
		return
	}
	inner := w - 6
	f.width = w
	f.title.Width = inner - 1
	f.start.Width = inner - 1
	f.end.Width = inner - 1
	f.body.SetWidth(inner - 2) // room for the two-space indent in view()
}

func (f *formModel) blurAll() {
	f.title.Blur()
	f.body.Blur()
	f.start.Blur()
	f.end.Blur()
}

func (f *formModel) focusCurrent() tea.Cmd {
	f.blurAll()
	switch f.focus {
	case fldTitle:
		return f.title.Focus()
	case fldBody:
		return f.body.Focus()
	case fldStart:
		return f.start.Focus()
	case fldEnd:
		return f.end.Focus()
	}
	return nil
}

func (m Model) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := &m.form
	if f.busy {
		if msg.String() == "esc" {
			m.overlay = overlayNone
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		return m, nil
	case "ctrl+s":
		return m.submitForm()
	case "tab", "ctrl+n":
		f.focus = (f.focus + 1) % fldCount
		return m, f.focusCurrent()
	case "shift+tab", "ctrl+p":
		f.focus = (f.focus + fldCount - 1) % fldCount
		return m, f.focusCurrent()
	case "enter":
		// Enter advances through the single-line fields but inserts a newline
		// in the description, where multi-line text is the point.
		if f.focus != fldBody {
			if f.focus == fldEnd {
				return m.submitForm()
			}
			f.focus = (f.focus + 1) % fldCount
			return m, f.focusCurrent()
		}
	}

	// Picker fields consume the arrow keys instead of the text inputs.
	switch f.focus {
	case fldLabels:
		switch msg.String() {
		case "left":
			if f.labelCursor > 0 {
				f.labelCursor--
			}
			return m, nil
		case "right":
			if f.labelCursor < len(f.labels)-1 {
				f.labelCursor++
			}
			return m, nil
		case " ", "x":
			if f.labelCursor < len(f.labels) {
				f.labels[f.labelCursor].on = !f.labels[f.labelCursor].on
			}
			return m, nil
		}
		return m, nil
	case fldStatus:
		if d := cycleDelta(msg.String()); d != 0 && len(f.statusOpts) > 0 {
			f.statusIdx = wrap(f.statusIdx+d, len(f.statusOpts))
		}
		return m, nil
	case fldPriority:
		if d := cycleDelta(msg.String()); d != 0 && len(f.prioOpts) > 0 {
			// -1 represents "unset", so the cycle is one longer than the options.
			f.prioIdx = wrap(f.prioIdx+1+d, len(f.prioOpts)+1) - 1
		}
		return m, nil
	}

	var cmd tea.Cmd
	switch f.focus {
	case fldTitle:
		f.title, cmd = f.title.Update(msg)
	case fldBody:
		f.body, cmd = f.body.Update(msg)
	case fldStart:
		f.start, cmd = f.start.Update(msg)
	case fldEnd:
		f.end, cmd = f.end.Update(msg)
	}
	return m, cmd
}

// indent shifts every line right, aligning the multi-line description with the
// single-line inputs above and below it.
func indent(s string, n int) string {
	pre := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pre + l
	}
	return strings.Join(lines, "\n")
}

func cycleDelta(key string) int {
	switch key {
	case "left", "shift+left":
		return -1
	case "right", "shift+right", " ":
		return 1
	}
	return 0
}

func wrap(v, n int) int {
	if n <= 0 {
		return 0
	}
	return ((v % n) + n) % n
}

func (m Model) submitForm() (tea.Model, tea.Cmd) {
	f := &m.form
	title := strings.TrimSpace(f.title.Value())
	if title == "" {
		f.err = "title is required"
		f.focus = fldTitle
		return m, f.focusCurrent()
	}

	start, err := parseDateInput(f.start.Value(), m.now)
	if err != nil {
		f.err = "start date: " + err.Error()
		f.focus = fldStart
		return m, f.focusCurrent()
	}
	end, err := parseDateInput(f.end.Value(), m.now)
	if err != nil {
		f.err = "due date: " + err.Error()
		f.focus = fldEnd
		return m, f.focusCurrent()
	}
	if start != nil && end != nil && end.Before(*start) {
		f.err = "due date is before the start date"
		f.focus = fldEnd
		return m, f.focusCurrent()
	}

	task := gh.NewTask{
		Title: title,
		Body:  strings.TrimSpace(f.body.Value()),
		Start: start,
		End:   end,
	}
	for _, l := range f.labels {
		if l.on {
			task.Labels = append(task.Labels, l.name)
		}
	}
	if len(f.statusOpts) > 0 {
		task.Status = f.statusOpts[f.statusIdx].Name
	}
	if f.prioIdx >= 0 && f.prioIdx < len(f.prioOpts) {
		task.Priority = f.prioOpts[f.prioIdx].Name
	}

	f.err = ""
	f.busy = true
	m.status = "creating task…"
	m.statusOK = false
	return m, m.createTask(task)
}

// parseDateInput accepts an ISO date, a few English relative forms, or blank.
func parseDateInput(s string, now time.Time) (*time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return nil, nil
	}
	today := startOfDay(now)
	switch s {
	case "today":
		return &today, nil
	case "tomorrow", "tmr":
		t := today.AddDate(0, 0, 1)
		return &t, nil
	case "yesterday":
		t := today.AddDate(0, 0, -1)
		return &t, nil
	}
	// +Nd / +Nw / +Nm offsets from today.
	if len(s) >= 2 && (s[0] == '+' || s[0] == '-') {
		unit := s[len(s)-1]
		numPart := s[1 : len(s)-1]
		if unit >= '0' && unit <= '9' {
			unit = 'd'
			numPart = s[1:]
		}
		n, err := strconv.Atoi(numPart)
		if err == nil {
			if s[0] == '-' {
				n = -n
			}
			var t time.Time
			switch unit {
			case 'd':
				t = today.AddDate(0, 0, n)
			case 'w':
				t = today.AddDate(0, 0, 7*n)
			case 'm':
				t = today.AddDate(0, n, 0)
			default:
				return nil, fmt.Errorf("unknown unit %q (use d, w, or m)", string(unit))
			}
			return &t, nil
		}
	}
	for _, layout := range []string{"2006-01-02", "2006/01/02", "01-02", "01/02"} {
		t, err := time.ParseInLocation(layout, s, now.Location())
		if err != nil {
			continue
		}
		if t.Year() == 0 {
			t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		}
		return &t, nil
	}
	return nil, fmt.Errorf("use YYYY-MM-DD, today, tomorrow, or +3d")
}

func (f formModel) view() string {
	if !f.ready {
		return ""
	}
	inner := f.width - 6

	label := func(idx int, text string) string {
		s := styMuted
		marker := "  "
		if f.focus == idx {
			s = styAccent.Bold(true)
			marker = styAccent.Render("▸ ")
		}
		return marker + s.Render(text)
	}

	var b strings.Builder
	b.WriteString(styTitle.Render("New task") + "  " + styMuted.Render("→ "+f.repoName) + "\n")
	b.WriteString(styMuted.Render(strings.Repeat("─", inner)) + "\n")

	b.WriteString(label(fldTitle, "Title") + "\n")
	b.WriteString("  " + f.title.View() + "\n\n")

	b.WriteString(label(fldBody, "Description") + "\n")
	b.WriteString(indent(f.body.View(), 2) + "\n\n")

	b.WriteString(label(fldLabels, "Labels") + "\n")
	b.WriteString("  " + f.labelsView(inner-2) + "\n\n")

	b.WriteString(label(fldStatus, "Status") + "  " + optionsView(f.statusOpts, f.statusIdx, f.focus == fldStatus, false) + "\n")
	b.WriteString(label(fldPriority, "Priority") + "  " + optionsView(f.prioOpts, f.prioIdx, f.focus == fldPriority, true) + "\n\n")

	b.WriteString(label(fldStart, "Start date") + "\n")
	b.WriteString("  " + f.start.View() + "\n\n")
	b.WriteString(label(fldEnd, "Due date") + "\n")
	b.WriteString("  " + f.end.View() + "\n")

	b.WriteString(styMuted.Render(strings.Repeat("─", inner)) + "\n")
	switch {
	case f.busy:
		b.WriteString(styAccent.Render("creating…"))
	case f.err != "":
		b.WriteString(styDanger.Render("! " + f.err))
	default:
		b.WriteString(keyHint(
			[2]string{"tab", "next field"},
			[2]string{"←→", "choose"},
			[2]string{"space", "toggle label"},
			[2]string{"ctrl+s", "create"},
			[2]string{"esc", "cancel"}))
	}

	return styOverlay.Width(f.width).Render(b.String())
}

// labelsView renders the label chips, wrapping onto extra lines as needed and
// marking the sub-cursor when the labels field has focus.
func (f formModel) labelsView(width int) string {
	if len(f.labels) == 0 {
		return styMuted.Render("(this repository has no labels)")
	}
	focused := f.focus == fldLabels

	var lines []string
	var line string
	lineW := 0
	for i, l := range f.labels {
		text := l.name
		if l.on {
			text = "✓" + text
		} else {
			text = " " + text
		}
		if focused && i == f.labelCursor {
			text = "[" + text + "]"
		} else {
			text = " " + text + " "
		}

		style := lipgloss.NewStyle().Foreground(hexColor(l.color))
		if l.on {
			style = style.Bold(true)
		} else if !focused {
			style = style.Faint(true)
		}
		chip := style.Render(text)

		w := lipgloss.Width(chip)
		if lineW+w > width && line != "" {
			lines = append(lines, line)
			line, lineW = "", 0
		}
		line += chip
		lineW += w
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n  ")
}

// optionsView renders a single-select as an inline chooser. When allowUnset is
// true an extra "—" chip represents leaving the field empty, which idx -1 selects.
func optionsView(opts []gh.Option, idx int, focused, allowUnset bool) string {
	if len(opts) == 0 {
		return styMuted.Render("(not available on this project)")
	}
	var parts []string
	render := func(name, color string, on bool) {
		style := lipgloss.NewStyle().Foreground(ghColor(color))
		switch {
		case on && focused:
			style = style.Bold(true).Underline(true)
		case on:
			style = style.Bold(true)
		default:
			style = style.Faint(true)
		}
		parts = append(parts, style.Render(name))
	}
	if allowUnset {
		render("—", "GRAY", idx < 0)
	}
	for i, o := range opts {
		render(o.Name, o.Color, i == idx)
	}
	return strings.Join(parts, styMuted.Render(" / "))
}
