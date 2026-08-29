// Package ui implements the terminal interface: a board, a roadmap, and a
// calendar over a GitHub Project and Google Calendar feeds.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/NaoyaTatetsu/toolbox-tui/internal/cache"
	"github.com/NaoyaTatetsu/toolbox-tui/internal/config"
	gh "github.com/NaoyaTatetsu/toolbox-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/toolbox-tui/internal/google-calendar"
)

type viewID int

const (
	viewBoard viewID = iota
	viewRoadmap
	viewCalendar
)

func (v viewID) String() string {
	switch v {
	case viewBoard:
		return "Board"
	case viewRoadmap:
		return "Roadmap"
	default:
		return "Calendar"
	}
}

type overlayID int

const (
	overlayNone overlayID = iota
	overlayHelp
	overlayDetail // a task, from the board or the roadmap
	overlayEvent  // a calendar event, from the agenda
	overlayForm
)

// Cache entry names. Keys include the project so switching boards cannot show
// another project's cards.
const (
	cacheEvents = "events"
)

// calendarWindow is how far around today calendar feeds are expanded. Recurring
// events are materialised once over this range instead of per month, so paging
// through months costs nothing.
const (
	calendarPast   = -6 * 30 * 24 * time.Hour
	calendarFuture = 12 * 30 * 24 * time.Hour
)

// Model is the root Bubble Tea model.
type Model struct {
	cfg      *config.Config
	gh       *gh.Client
	calendar *googlecalendar.Client

	project *gh.Project
	repo    *gh.Repo
	events  []googlecalendar.Event

	view   viewID
	width  int
	height int

	board   boardState
	roadmap roadmapState
	month   calendarState

	overlay overlayID
	form    formModel
	detail  detailState
	event   eventState
	scroll  scrollState

	pending int // outstanding async loads
	// projectStale / eventsStale mark data that came off disk and has not yet
	// been confirmed by the network.
	projectStale bool
	eventsStale  bool
	spinner      int
	status       string
	statusOK     bool
	errs         []string
	now          time.Time
	quitting     bool

	// scrollLevel indexes config.ScrollPresets. It starts from whatever the
	// config asks for and moves with the , and . keys.
	scrollLevel int

	// hits is filled by the renderer each frame and read when a click arrives.
	hits *hitmap

	// clock is overridable so the scroll cooldown can be tested without sleeping.
	clock func() time.Time
}

func (m Model) timeNow() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// New builds the root model. Data loading happens in Init so the first frame
// paints immediately.
func New(cfg *config.Config, ghClient *gh.Client, calendarClient *googlecalendar.Client) Model {
	now := time.Now()
	m := Model{
		cfg:      cfg,
		gh:       ghClient,
		calendar: calendarClient,
		view:     viewBoard,
		now:      now,
	}
	m.month = newCalendarState(now)
	m.roadmap = newRoadmapState(now, cfg.UI.RoadmapDays)
	m.hits = &hitmap{}
	m.scrollLevel = config.NearestScrollLevel(cfg.UI.TicksPerStep(), int(cfg.UI.ScrollInterval().Milliseconds()))
	// One for the project, one for the repo if configured, one for the calendar.
	m.pending = 1
	if _, _, ok := cfg.RepoParts(); ok {
		m.pending++
	}
	if len(cfg.Calendar.Sources) > 0 {
		m.pending++
	}
	return m
}

// ---- messages ----

type projectMsg struct {
	project *gh.Project
	err     error
}

type repoMsg struct {
	repo *gh.Repo
	err  error
}

type eventsMsg struct {
	events []googlecalendar.Event
	errs   []error
}

type taskCreatedMsg struct {
	issue *gh.CreatedIssue
	err   error
}

type statusMovedMsg struct {
	itemID string
	status string
	err    error
}

type cacheMsg struct {
	project *gh.Project
	events  []googlecalendar.Event
	at      time.Time
}

type tickMsg time.Time

type clearStatusMsg struct{}

// ---- commands ----

func (m Model) loadProject() tea.Cmd {
	cfg := m.cfg
	client := m.gh
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		p, err := client.FetchProject(ctx, cfg.GitHub.OwnerType, cfg.GitHub.Owner, cfg.GitHub.ProjectNumber)
		return projectMsg{project: p, err: err}
	}
}

// projectCacheKey namespaces the entry by owner and project number.
func (m Model) projectCacheKey() string {
	return fmt.Sprintf("project-%s-%d", m.cfg.GitHub.Owner, m.cfg.GitHub.ProjectNumber)
}

// loadCache reads the last successful fetch off disk. It is a separate command
// from the network fetches so the first frame can show real cards while the
// refresh is still in flight.
func (m Model) loadCache() tea.Cmd {
	key := m.projectCacheKey()
	return func() tea.Msg {
		var msg cacheMsg
		var project gh.Project
		if at, err := cache.Load(key, &project); err == nil {
			msg.project = &project
			msg.at = at
		}
		var events []googlecalendar.Event
		if at, err := cache.Load(cacheEvents, &events); err == nil {
			msg.events = events
			if msg.at.IsZero() || at.Before(msg.at) {
				msg.at = at
			}
		}
		return msg
	}
}

// saveCache persists a fetch. Failures are ignored: a cache that cannot be
// written costs a slower next start and nothing else.
func saveCache(name string, v any) tea.Cmd {
	return func() tea.Msg {
		_ = cache.Save(name, v)
		return nil
	}
}

func (m Model) loadRepo() tea.Cmd {
	owner, name, ok := m.cfg.RepoParts()
	if !ok {
		return nil
	}
	client := m.gh
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r, err := client.FetchRepo(ctx, owner, name)
		return repoMsg{repo: r, err: err}
	}
}

func (m Model) loadEvents() tea.Cmd {
	sources := make([]googlecalendar.Source, 0, len(m.cfg.Calendar.Sources))
	for _, s := range m.cfg.Calendar.Sources {
		name := s.Name
		if name == "" {
			name = "calendar"
		}
		sources = append(sources, googlecalendar.Source{Name: name, URL: s.URL, Color: s.Color, Email: s.Email})
	}
	if len(sources) == 0 {
		return nil
	}
	client := m.calendar
	now := m.now
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		from := now.Add(calendarPast)
		to := now.Add(calendarFuture)
		evs, errs := client.Events(ctx, sources, from, to, time.Local)
		return eventsMsg{events: evs, errs: errs}
	}
}

func (m Model) moveStatus(item gh.Item, status string) tea.Cmd {
	client, project := m.gh, m.project
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := client.MoveStatus(ctx, project, item.ID, status)
		return statusMovedMsg{itemID: item.ID, status: status, err: err}
	}
}

func (m Model) createTask(t gh.NewTask) tea.Cmd {
	client, project, repo := m.gh, m.project, m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		issue, err := client.CreateTask(ctx, project, repo, t)
		return taskCreatedMsg{issue: issue, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

// openURL launches the platform browser. Failures are silent by design: the
// URL is already on screen in the detail pane.
func openURL(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		_ = cmd.Start()
		return nil
	}
}

// ---- Bubble Tea ----

func (m Model) Init() tea.Cmd {
	// The palette needs to know which way the terminal leans. lipgloss v2 no
	// longer asks on its own, so the answer is requested here and lands as a
	// tea.BackgroundColorMsg.
	cmds := []tea.Cmd{tea.RequestBackgroundColor, m.loadCache(), m.loadProject(), tick()}
	if c := m.loadRepo(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.loadEvents(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func (m *Model) refresh() tea.Cmd {
	m.errs = nil
	m.status = "refreshing…"
	m.statusOK = false
	m.calendar.Invalidate()
	m.now = time.Now()
	cmds := []tea.Cmd{m.loadProject()}
	if c := m.loadRepo(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.loadEvents(); c != nil {
		cmds = append(cmds, c)
	}
	m.pending += len(cmds)
	return tea.Batch(cmds...)
}

// humanAge renders a cache age the way a person would say it.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.form.setWidth(m.formWidth())
		return m, nil

	case tickMsg:
		m.spinner++
		return m, tick()

	case clearStatusMsg:
		m.status = ""
		return m, nil

	case cacheMsg:
		// Only fill gaps: a network response that already landed always wins.
		var used bool
		if m.project == nil && msg.project != nil {
			m.project = msg.project
			m.projectStale = true
			m.rebuild()
			used = true
		}
		if len(m.events) == 0 && len(msg.events) > 0 {
			m.events = msg.events
			m.eventsStale = true
			used = true
		}
		if used {
			m.status = "showing cached data from " + humanAge(m.now.Sub(msg.at))
		}
		return m, nil

	case projectMsg:
		m.pending--
		if msg.err != nil {
			m.errs = append(m.errs, msg.err.Error())
			return m, nil
		}
		m.project = msg.project
		m.projectStale = false
		m.rebuild()
		cmds := []tea.Cmd{saveCache(m.projectCacheKey(), m.project)}
		if m.pending <= 0 {
			m.status = fmt.Sprintf("%d tasks loaded", len(m.project.Items))
			m.statusOK = true
			cmds = append(cmds, clearStatusAfter(4*time.Second))
		}
		return m, tea.Batch(cmds...)

	case repoMsg:
		m.pending--
		if msg.err != nil {
			m.errs = append(m.errs, "repo: "+msg.err.Error())
			return m, nil
		}
		m.repo = msg.repo
		return m, nil

	case eventsMsg:
		m.pending--
		for _, e := range msg.errs {
			m.errs = append(m.errs, "calendar: "+e.Error())
		}
		if len(msg.events) == 0 && len(msg.errs) > 0 {
			// Every source failed; keep whatever the cache gave us.
			return m, nil
		}
		m.events = msg.events
		m.eventsStale = false
		return m, saveCache(cacheEvents, m.events)

	case statusMovedMsg:
		if msg.err != nil {
			m.errs = append(m.errs, msg.err.Error())
			m.status = "move failed"
			m.statusOK = false
			return m, clearStatusAfter(6 * time.Second)
		}
		m.status = "moved to " + msg.status
		m.statusOK = true
		return m, clearStatusAfter(3 * time.Second)

	case taskCreatedMsg:
		if msg.err != nil {
			m.errs = append(m.errs, msg.err.Error())
			if msg.issue == nil {
				m.status = "create failed"
				m.statusOK = false
				return m, clearStatusAfter(6 * time.Second)
			}
		}
		m.overlay = overlayNone
		if msg.issue != nil {
			m.status = fmt.Sprintf("created #%d", msg.issue.Number)
			m.statusOK = true
		}
		return m, tea.Batch(m.refresh(), clearStatusAfter(5*time.Second))

	case tea.BackgroundColorMsg:
		darkBackground = msg.IsDark()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		if mouse := msg.Mouse(); mouse.Button == tea.MouseLeft {
			return m.handleClick(mouse.X, mouse.Y)
		}

	case tea.MouseWheelMsg:
		m, key, ok := m.accumulateScroll(msg.Mouse(), m.timeNow())
		if !ok {
			return m, nil
		}
		return m.handleKey(key)
	}
	return m, nil
}

// handleClick selects whatever was drawn under the pointer. The hit map comes
// from the frame the user was actually looking at when they clicked.
func (m Model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		// Clicks are inert while an overlay covers the board.
		return m, nil
	}
	region, ok := m.hits.at(x, y)
	if !ok {
		return m, nil
	}
	switch region.kind {
	case hitBoardCard:
		m.board.col, m.board.row = region.col, region.row
		m.board.clamp()
	case hitRoadmapRow:
		m.roadmap.cursor = clamp(region.index, 0, max(0, len(m.roadmap.items)-1))
	case hitCalendarDay:
		m.month.moveTo(region.day)
	case hitAgendaEntry:
		m.month.agenda = region.index
		m.month.focus = focusAgenda
	}
	return m, nil
}

// scrollState counts wheel events that have not yet earned a movement, one
// accumulator per axis, plus when the last step was emitted.
type scrollState struct {
	vertical   int
	horizontal int
	lastStep   time.Time
}

// accumulateScroll folds a wheel event into the per-axis accumulator and, once
// enough have arrived and enough time has passed, returns the arrow key it
// stands for. Routing the result through the keyboard handler keeps pointer and
// key input from drifting apart as bindings change.
//
// Two limits work together, because a trackpad needs both. Spending several
// events per step matches one step to one visible flick. But the trackpad's
// momentum keeps emitting events long after your fingers leave the glass, and
// no per-event count can throttle a stream that long — so a cooldown caps how
// often a step can be produced at all.
//
// Horizontal wheel events (a two-finger sideways swipe) are only reported by
// terminals that send SGR buttons 6 and 7 — iTerm2, Ghostty, WezTerm, Alacritty
// and Kitty do; macOS Terminal.app does not. Shift plus a vertical scroll is
// accepted as the fallback those terminals leave available.
func (m Model) accumulateScroll(event tea.Mouse, now time.Time) (Model, tea.KeyPressMsg, bool) {
	shift := event.Mod.Contains(tea.ModShift)

	var horizontal bool
	var delta int
	switch event.Button {
	case tea.MouseWheelUp:
		horizontal, delta = shift, -1
	case tea.MouseWheelDown:
		horizontal, delta = shift, +1
	case tea.MouseWheelLeft:
		horizontal, delta = true, -1
	case tea.MouseWheelRight:
		horizontal, delta = true, +1
	default:
		return m, tea.KeyPressMsg{}, false
	}

	// Inside the cooldown, discard the event outright rather than banking it, so
	// a long momentum tail cannot pile up steps to release in a burst.
	if interval := m.cfg.UI.ScrollInterval(); interval > 0 &&
		!m.scroll.lastStep.IsZero() && now.Sub(m.scroll.lastStep) < interval {
		return m, tea.KeyPressMsg{}, false
	}

	acc := &m.scroll.vertical
	other := &m.scroll.horizontal
	if horizontal {
		acc, other = other, acc
	}
	// Reversing direction, or changing axis, abandons any partial count so a
	// flick back the other way responds straight away.
	if (*acc > 0) != (delta > 0) {
		*acc = 0
	}
	*other = 0
	*acc += delta

	if abs(*acc) < m.cfg.UI.TicksPerStep() {
		return m, tea.KeyPressMsg{}, false
	}
	*acc = 0
	m.scroll.lastStep = now

	switch {
	case horizontal && delta < 0:
		return m, tea.KeyPressMsg{Code: tea.KeyLeft}, true
	case horizontal:
		return m, tea.KeyPressMsg{Code: tea.KeyRight}, true
	case delta < 0:
		return m, tea.KeyPressMsg{Code: tea.KeyUp}, true
	default:
		return m, tea.KeyPressMsg{Code: tea.KeyDown}, true
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Overlays get first refusal on every key.
	switch m.overlay {
	case overlayForm:
		return m.updateForm(msg)
	case overlayDetail:
		switch msg.String() {
		case "esc", "q", "enter":
			m.overlay = overlayNone
			return m, nil
		case "o":
			return m, openURL(m.detail.item.URL)
		case "down":
			m.detail.scroll++
			return m, nil
		case "up":
			if m.detail.scroll > 0 {
				m.detail.scroll--
			}
			return m, nil
		}
		return m, nil
	case overlayEvent:
		switch msg.String() {
		case "esc", "q", "enter":
			m.overlay = overlayNone
			return m, nil
		case "o":
			if link := eventLink(m.event.event); link != "" {
				return m, openURL(link)
			}
			return m, nil
		case "down":
			m.event.scroll++
			return m, nil
		case "up":
			if m.event.scroll > 0 {
				m.event.scroll--
			}
			return m, nil
		}
		return m, nil
	case overlayHelp:
		m.overlay = overlayNone
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.overlay = overlayHelp
		return m, nil
	case "r":
		return m, m.refresh()
	case "1":
		m.view = viewBoard
		return m, nil
	case "2":
		m.view = viewRoadmap
		return m, nil
	case "3":
		m.view = viewCalendar
		return m, nil
	case "]", "tab":
		m.view = (m.view + 1) % 3
		return m, nil
	case "[", "shift+tab":
		m.view = (m.view + 2) % 3
		return m, nil
	case "n":
		return m.openForm()
	case ",":
		return m.adjustScroll(-1)
	case ".":
		return m.adjustScroll(+1)
	}

	switch m.view {
	case viewBoard:
		return m.updateBoard(msg)
	case viewRoadmap:
		return m.updateRoadmap(msg)
	default:
		return m.updateCalendar(msg)
	}
}

// adjustScroll steps the sensitivity ladder and reports where it landed,
// including the config line to keep. Tuning this by feel needs immediate
// feedback; describing two numbers in a README does not provide any.
func (m Model) adjustScroll(dir int) (tea.Model, tea.Cmd) {
	level := clamp(m.scrollLevel+dir, 0, len(config.ScrollPresets)-1)
	if level == m.scrollLevel {
		edge := "slowest"
		if dir > 0 {
			edge = "fastest"
		}
		m.status = "scroll already at its " + edge
		m.statusOK = false
		return m, clearStatusAfter(3 * time.Second)
	}
	m.scrollLevel = level
	preset := config.ScrollPresets[level]
	m.cfg.UI.ScrollTicks = preset.Ticks
	interval := preset.IntervalMS
	m.cfg.UI.ScrollIntervalMS = &interval
	// A partial count from the old setting would misbehave under the new one.
	m.scroll = scrollState{}

	rate := "uncapped"
	if sps := preset.StepsPerSecond(); sps > 0 {
		rate = fmt.Sprintf("max %d/s", sps)
	}
	m.status = fmt.Sprintf("scroll %d/%d (%s) — scroll_ticks = %d, scroll_interval_ms = %d",
		level+1, len(config.ScrollPresets), rate, preset.Ticks, preset.IntervalMS)
	m.statusOK = true
	return m, clearStatusAfter(8 * time.Second)
}

// rebuild recomputes derived view state after the project reloads, keeping the
// selection on the same task where possible.
func (m *Model) rebuild() {
	if m.project == nil {
		return
	}
	cols := m.project.Board(m.cfg.UI.StatusOrder, m.cfg.UI.HideDone, m.now)
	m.board.setColumns(cols)
	m.roadmap.setItems(m.project.Scheduled(), m.now)
}

// View hands Bubble Tea the frame together with the terminal modes the program
// wants. v2 made those properties of the view rather than options passed to the
// program, so they are decided here, once per frame.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if m.cfg.UI.MouseEnabled() {
		// Cell motion is the lightest mode that still reports wheel events.
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// render draws one frame.
func (m Model) render() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "loading…"
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	// Regions are recorded fresh every frame; an overlay leaves the map empty so
	// clicks cannot reach the board underneath it.
	m.hits.reset()
	bodyTop := lipgloss.Height(header)

	var body string
	switch {
	case m.project == nil && m.view != viewCalendar:
		body = m.renderLoading(bodyHeight)
	case m.view == viewBoard:
		body = m.renderBoard(m.width, bodyHeight, bodyTop)
	case m.view == viewRoadmap:
		body = m.renderRoadmap(m.width, bodyHeight, bodyTop)
	default:
		body = m.renderCalendar(m.width, bodyHeight, bodyTop)
	}
	body = lipgloss.NewStyle().Width(m.width).Height(bodyHeight).MaxHeight(bodyHeight).Render(body)

	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.overlay != overlayNone {
		m.hits.reset()
	}
	if m.overlay == overlayHelp {
		return m.overlayOn(screen, m.renderHelp())
	}
	if m.overlay == overlayDetail {
		return m.overlayOn(screen, m.renderDetail())
	}
	if m.overlay == overlayEvent {
		return m.overlayOn(screen, m.renderEventDetail())
	}
	if m.overlay == overlayForm {
		return m.overlayOn(screen, m.form.view())
	}
	return screen
}

// overlayOn centres a panel over the base screen.
func (m Model) overlayOn(base, panel string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		panel, lipgloss.WithWhitespaceChars(" "))
}

func (m Model) renderHeader() string {
	title := "toolbox-tui"
	if m.project != nil {
		title = m.project.Title
	}
	left := styTitle.Render(title) + " " +
		styMuted.Render(fmt.Sprintf("%s/%d", m.cfg.GitHub.Owner, m.cfg.GitHub.ProjectNumber))

	// Tabs shed their labels before the title gives up any room, since the
	// digits alone still say which view is active.
	tabs := m.renderTabs(true)
	if lipgloss.Width(tabs)+8 > m.width {
		tabs = m.renderTabs(false)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(tabs)
	if gap < 1 {
		left = truncate(left, max(0, m.width-lipgloss.Width(tabs)-1))
		gap = max(0, m.width-lipgloss.Width(left)-lipgloss.Width(tabs))
	}
	line := left + strings.Repeat(" ", gap) + tabs
	rule := styMuted.Render(strings.Repeat("─", max(0, m.width)))
	return truncate(line, m.width) + "\n" + rule
}

func (m Model) renderTabs(withLabels bool) string {
	var tabs []string
	for _, v := range []viewID{viewBoard, viewRoadmap, viewCalendar} {
		label := fmt.Sprintf(" %d ", int(v)+1)
		if withLabels {
			label = fmt.Sprintf(" %d %s ", int(v)+1, v)
		}
		if v == m.view {
			tabs = append(tabs, styTabOn.Render(label))
		} else {
			tabs = append(tabs, styTabOff.Render(label))
		}
	}
	return strings.Join(tabs, "")
}

func (m Model) renderFooter() string {
	rule := styMuted.Render(strings.Repeat("─", m.width))

	var hints string
	switch m.view {
	case viewBoard:
		hints = keyHint([2]string{"[]", "view"}, [2]string{"←↑↓→", "move"},
			[2]string{"HL", "change status"}, [2]string{"n", "new"},
			[2]string{"enter", "detail"}, [2]string{"o", "open"}, [2]string{"?", "help"})
	case viewRoadmap:
		hints = keyHint([2]string{"[]", "view"}, [2]string{"↑↓", "select"},
			[2]string{"←→", "scroll"}, [2]string{"-+", "zoom"}, [2]string{"t", "today"},
			[2]string{"enter", "detail"}, [2]string{"?", "help"})
	default:
		if m.month.focus == focusAgenda {
			hints = keyHint([2]string{"[]", "view"}, [2]string{"↑↓", "select"},
				[2]string{"enter", "detail"}, [2]string{"esc", "back to the grid"},
				[2]string{"?", "help"})
			break
		}
		hints = keyHint([2]string{"[]", "view"}, [2]string{"←↑↓→", "day/week"},
			[2]string{"HL", "month"}, [2]string{"t", "today"},
			[2]string{"enter", "open the day"}, [2]string{"?", "help"})
	}

	status := ""
	switch {
	case m.pending > 0:
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		label := " loading"
		if m.projectStale || m.eventsStale {
			label = " refreshing"
		}
		status = styAccent.Render(frames[m.spinner%len(frames)] + label)
	case len(m.errs) > 0:
		status = styDanger.Render("! " + truncate(firstLine(m.errs[len(m.errs)-1]), max(10, m.width/2)))
	case m.status != "" && m.statusOK:
		status = styOK.Render("✓ " + m.status)
	case m.status != "":
		status = styStatus.Render(m.status)
	}

	gap := m.width - lipgloss.Width(hints) - lipgloss.Width(status)
	if gap < 1 {
		hints = truncate(hints, max(0, m.width-lipgloss.Width(status)-1))
		gap = 1
	}
	return rule + "\n" + hints + strings.Repeat(" ", gap) + status
}

func (m Model) renderLoading(height int) string {
	msg := styMuted.Render("loading project…")
	if len(m.errs) > 0 {
		msg = styDanger.Render(strings.Join(m.errs, "\n\n"))
	}
	return lipgloss.Place(m.width, height, lipgloss.Center, lipgloss.Center, msg)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
