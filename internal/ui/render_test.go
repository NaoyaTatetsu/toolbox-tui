package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/NaoyaTatetsu/toolbox-tui/internal/config"
	gh "github.com/NaoyaTatetsu/toolbox-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/toolbox-tui/internal/google-calendar"
)

var testNow = time.Date(2026, 8, 28, 11, 0, 0, 0, time.Local)

func day(y int, mo time.Month, d int) *time.Time {
	t := time.Date(y, mo, d, 0, 0, 0, 0, time.Local)
	return &t
}

func testProject() *gh.Project {
	p := &gh.Project{
		ID: "PVT_x", Title: "Tasks", Number: 4,
		Fields: []gh.Field{
			{ID: "f1", Name: "Status", DataType: gh.FieldSingleSelect, Options: []gh.Option{
				{ID: "s1", Name: "Pending", Color: "BLUE"},
				{ID: "s2", Name: "Todo", Color: "GREEN"},
				{ID: "s3", Name: "In Progress", Color: "YELLOW"},
				{ID: "s4", Name: "In Review", Color: "PINK"},
				{ID: "s5", Name: "Done", Color: "PURPLE"},
			}},
			{ID: "f2", Name: "Priority", DataType: gh.FieldSingleSelect, Options: []gh.Option{
				{ID: "p1", Name: "High", Color: "RED"},
				{ID: "p2", Name: "Middle", Color: "YELLOW"},
				{ID: "p3", Name: "Low", Color: "BLUE"},
			}},
			{ID: "f3", Name: "Start Date", DataType: gh.FieldDate},
			{ID: "f4", Name: "End Date", DataType: gh.FieldDate},
		},
		Items: []gh.Item{
			{ID: "i1", Type: "ISSUE", Number: 101, Title: "設計レビューの反映", Repo: "example/notes",
				State: "OPEN", Status: "Pending", Priority: "Middle",
				StartDate: day(2026, 8, 3), EndDate: day(2026, 8, 14),
				Labels: []gh.Label{{Name: "Develop", Color: "5319e7"}}, Extra: map[string]string{}},
			{ID: "i2", Type: "ISSUE", Number: 102, Title: "請求書の確認", Repo: "example/notes",
				State: "OPEN", Status: "In Review", Priority: "High", EndDate: day(2026, 8, 27),
				Labels: []gh.Label{{Name: "Chore", Color: "c5def5"}}, Extra: map[string]string{}},
			{ID: "i3", Type: "ISSUE", Number: 103, Title: "Write the quarterly report and circulate it widely",
				Repo: "example/notes", State: "OPEN", Status: "In Progress", Priority: "Low",
				StartDate: day(2026, 8, 26), EndDate: day(2026, 9, 10),
				Body:  "First paragraph.\n\nSecond paragraph with rather a lot of words in it so that wrapping has to do some work.",
				Extra: map[string]string{}},
			{ID: "i4", Type: "ISSUE", Number: 104, Title: "経費の集計", Repo: "example/notes",
				State: "CLOSED", Status: "Done", Priority: "Middle",
				StartDate: day(2026, 8, 14), EndDate: day(2026, 8, 14), Extra: map[string]string{}},
			{ID: "i5", Type: "ISSUE", Number: 105, Title: "No dates, no priority", Repo: "example/notes",
				State: "OPEN", Status: "Todo", Extra: map[string]string{}},
			{ID: "i6", Type: "DRAFT_ISSUE", Title: "Draft with no issue behind it",
				Status: "Todo", Priority: "High", EndDate: day(2026, 8, 31), Extra: map[string]string{}},
			// A second Pending card, so vertical navigation has somewhere to go.
			{ID: "i7", Type: "ISSUE", Number: 107, Title: "Renew the domain", Repo: "example/notes",
				State: "OPEN", Status: "Pending", Extra: map[string]string{}},
		},
	}
	return p
}

func testEvents() []googlecalendar.Event {
	mk := func(h, m int, dur time.Duration, summary string) googlecalendar.Event {
		s := time.Date(2026, 8, 28, h, m, 0, 0, time.Local)
		return googlecalendar.Event{UID: summary, Summary: summary, Calendar: "personal", Start: s, End: s.Add(dur)}
	}
	allDay := googlecalendar.Event{
		UID: "trip", Summary: "Osaka trip", Calendar: "personal", AllDay: true,
		Start: time.Date(2026, 8, 27, 0, 0, 0, 0, time.Local),
		End:   time.Date(2026, 8, 30, 0, 0, 0, 0, time.Local),
	}
	oneOnOne := mk(14, 0, time.Hour, "1:1 with a very long meeting title")
	oneOnOne.Location = "Meeting room B"
	oneOnOne.Description = "Agenda:\n- last week\n- next week"
	oneOnOne.Repeat = "every week on Fri"
	oneOnOne.Conference = "https://meet.example.invalid/abc-defg-hij"
	oneOnOne.MyStatus = "TENTATIVE"
	oneOnOne.Organizer = googlecalendar.Attendee{Name: "Alice", Email: "alice@example.com"}
	oneOnOne.Attendees = []googlecalendar.Attendee{
		{Name: "Alice", Email: "alice@example.com", Status: "ACCEPTED"},
		{Name: "Me", Email: "me@example.com", Status: "TENTATIVE", Self: true},
		{Name: "Bob", Email: "bob@example.com", Status: "DECLINED"},
		{Name: "Room B", Email: "room@resource.invalid", Status: "ACCEPTED", Resource: true},
	}
	return []googlecalendar.Event{allDay, mk(10, 0, 30*time.Minute, "Standup"), oneOnOne}
}

func newTestModel(w, h int) Model {
	cfg := &config.Config{}
	cfg.GitHub.Owner = "example-user"
	cfg.GitHub.OwnerType = "user"
	cfg.GitHub.ProjectNumber = 4
	cfg.GitHub.DefaultRepo = "example/notes"
	cfg.UI.StatusOrder = []string{"Pending", "Todo", "In Progress", "In Review", "Done"}
	cfg.UI.RoadmapDays = 28
	cfg.Calendar.Sources = []config.CalendarSource{{Name: "personal", URL: "https://example.invalid/basic.ics"}}

	m := New(cfg, nil, nil)
	m.now = testNow
	m.month = newCalendarState(testNow)
	m.roadmap = newRoadmapState(testNow, 28)
	m.width, m.height = w, h
	m.project = testProject()
	m.events = testEvents()
	m.repo = &gh.Repo{ID: "R_x", Owner: "example", Name: "notes", Labels: []gh.RepoLabel{
		{ID: "l1", Name: "Develop", Color: "5319e7"},
		{ID: "l2", Name: "Chore", Color: "c5def5"},
		{ID: "l3", Name: "Bug", Color: "d73a4a"},
	}}
	m.rebuild()
	// Each read jumps a second forward, so the scroll cooldown never interferes
	// with tests that are about the tick threshold.
	tick := testNow
	m.clock = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	return m
}

// TestViewsRenderAtManySizes is the guard against panics and runaway layouts:
// every view must produce a frame that fits the terminal it was given.
func TestViewsRenderAtManySizes(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}, {60, 20}, {40, 14}, {30, 10}}
	for _, sz := range sizes {
		for _, view := range []viewID{viewBoard, viewRoadmap, viewCalendar} {
			m := newTestModel(sz[0], sz[1])
			m.view = view
			out := m.View()
			lines := strings.Split(out, "\n")
			if len(lines) > sz[1] {
				t.Errorf("%v at %dx%d: %d lines, want <= %d", view, sz[0], sz[1], len(lines), sz[1])
			}
			for i, l := range lines {
				if w := lipglossWidth(l); w > sz[0] {
					t.Errorf("%v at %dx%d: line %d is %d cells wide, want <= %d\n%q",
						view, sz[0], sz[1], i, w, sz[0], l)
					break
				}
			}
		}
	}
}

func TestOverlaysRender(t *testing.T) {
	for _, o := range []overlayID{overlayHelp, overlayDetail} {
		m := newTestModel(120, 40)
		m.overlay = o
		m.detail = detailState{item: m.project.Items[2]}
		if out := m.View(); out == "" {
			t.Errorf("overlay %v rendered empty", o)
		}
	}
	// The form is built by openForm so that it picks up the project's fields.
	m := newTestModel(120, 40)
	tm, _ := m.openForm()
	m = tm.(Model)
	if m.overlay != overlayForm {
		t.Fatalf("openForm did not open the form overlay")
	}
	out := m.View()
	for _, want := range []string{"New task", "Develop", "In Progress", "High"} {
		if !strings.Contains(out, want) {
			t.Errorf("form is missing %q", want)
		}
	}
}

func TestBoardNavigationAndOptimisticMove(t *testing.T) {
	m := newTestModel(120, 40)
	if got := len(m.board.cols); got != 5 {
		t.Fatalf("got %d columns, want 5", got)
	}
	// Pending has one card; move right to Todo.
	if it, ok := m.board.selected(); !ok || it.ID != "i1" {
		t.Fatalf("initial selection = %+v", it)
	}
	tm, _ := m.moveSelected(+1)
	m = tm.(Model)
	if len(m.board.cols[0].Items) != 1 {
		t.Errorf("Pending should have 1 card left, has %d", len(m.board.cols[0].Items))
	}
	for _, it := range m.board.cols[0].Items {
		if it.ID == "i1" {
			t.Error("the moved card is still in Pending")
		}
	}
	it, ok := m.board.selected()
	if !ok || it.ID != "i1" || it.Status != "Todo" {
		t.Errorf("after move, selection = %+v (col %d)", it, m.board.col)
	}
	if m.board.cols[1].Name != "Todo" || len(m.board.cols[1].Items) != 3 {
		t.Errorf("Todo column = %+v", m.board.cols[1])
	}
}

func TestSelectionSurvivesReload(t *testing.T) {
	m := newTestModel(120, 40)
	m.board.col, m.board.row = 3, 0 // In Review / #277
	want := m.board.selectedID()
	m.rebuild()
	if got := m.board.selectedID(); got != want {
		t.Errorf("selection moved from %s to %s across reload", want, got)
	}
}

// TestCalendarAgendaListsEventsOnly guards the split between the two halves of
// the tool: the day pane is a calendar, not a second task list.
func TestCalendarAgendaListsEventsOnly(t *testing.T) {
	m := newTestModel(120, 40)
	// #103 spans Aug 26 – Sep 10, so it covers Aug 28 and would have shown up
	// here back when tasks were merged into the agenda.
	events := m.agenda(time.Date(2026, 8, 28, 0, 0, 0, 0, time.Local))
	if len(events) != 3 {
		t.Errorf("got %d entries on Aug 28, want 3 (all-day trip + 2 timed events)", len(events))
	}
}

func TestParseDateInput(t *testing.T) {
	now := testNow
	cases := map[string]string{
		"":           "",
		"today":      "2026-08-28",
		"tomorrow":   "2026-08-29",
		"+3d":        "2026-08-31",
		"+2w":        "2026-09-11",
		"-1d":        "2026-08-27",
		"2026-12-01": "2026-12-01",
		"2026/12/01": "2026-12-01",
	}
	for in, want := range cases {
		got, err := parseDateInput(in, now)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if want == "" {
			if got != nil {
				t.Errorf("%q: got %v, want nil", in, got)
			}
			continue
		}
		if got == nil || got.Format("2006-01-02") != want {
			t.Errorf("%q: got %v, want %s", in, got, want)
		}
	}
	if _, err := parseDateInput("next thursday", now); err == nil {
		t.Error("expected an error for unparsable input")
	}
}

func TestTruncateIsWidthAware(t *testing.T) {
	// Japanese characters occupy two cells each.
	if got := lipglossWidth(truncate("あいうえお", 6)); got > 6 {
		t.Errorf("truncated to %d cells, want <= 6", got)
	}
	if got := lipglossWidth(pad("あい", 10)); got != 10 {
		t.Errorf("pad width = %d, want 10", got)
	}
}

// TestDumpFrames prints real frames for eyeballing: go test ./internal/ui -run Dump -v
func TestDumpFrames(t *testing.T) {
	if os.Getenv("DUMP") == "" {
		t.Skip("set DUMP=1 to print frames")
	}
	for _, view := range []viewID{viewBoard, viewRoadmap, viewCalendar} {
		m := newTestModel(120, 34)
		m.view = view
		fmt.Printf("\n===== %v =====\n%s\n", view, m.View())
	}
	m := newTestModel(120, 34)
	tm, _ := m.openForm()
	fmt.Printf("\n===== form =====\n%s\n", tm.(Model).View())
	m2 := newTestModel(120, 34)
	m2.overlay = overlayDetail
	m2.detail = detailState{item: m2.project.Items[2]}
	fmt.Printf("\n===== detail =====\n%s\n", m2.View())
}

var _ tea.Model = Model{}

// TestBoardColumnsHaveGutter guards the fix that stopped adjacent cards from
// sharing a border column.
func TestBoardColumnsHaveGutter(t *testing.T) {
	m := newTestModel(120, 40)
	out := m.renderBoard(120, 36, 2)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(stripANSI(line), "╮╭") || strings.Contains(stripANSI(line), "╯╰") {
			t.Fatalf("cards are touching:\n%s", stripANSI(line))
		}
	}
}

// TestRoadmapRowsAlign checks that every bar starts at the same column, which
// is easy to break with East-Asian titles in the label gutter.
func TestRoadmapRowsAlign(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewRoadmap
	lines := strings.Split(m.renderRoadmap(120, 30, 2), "\n")
	if len(lines) < 4 {
		t.Fatalf("roadmap rendered %d lines", len(lines))
	}
	want := lipglossWidth(lines[1]) // the ruler line spans label + timeline
	for i, l := range lines[2 : len(lines)-1] {
		if got := lipglossWidth(l); got != want {
			t.Errorf("row %d is %d cells wide, ruler is %d\n%q", i, got, want, stripANSI(l))
		}
	}
}

// TestAgendaTruncatesRatherThanWraps guards the fix where the agenda was being
// rendered at the grid's width and then wrapped over the calendar.
func TestAgendaTruncatesRatherThanWraps(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	out := stripANSI(m.renderCalendar(120, 30, 2))
	if strings.Contains(out, "1:1 with a very long meeting title") {
		t.Error("the long event title was not truncated to the agenda pane")
	}
	if !strings.Contains(out, "1:1 with") {
		t.Error("the long event is missing from the agenda entirely")
	}
	// The time column must not run into the summary.
	if strings.Contains(out, "10:00-10:30Standup") {
		t.Error("time column is missing its separating space")
	}
}

// lipglossWidth reports the rendered cell width of a line, ignoring ANSI codes.
func lipglossWidth(s string) int { return ansi.StringWidth(s) }

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// TestCalendarEnterOpensTheEvent walks the calendar's two steps: enter moves
// from the grid into the day pane, and enter again opens the row under the
// cursor as a detail overlay saying when and where it happens.
func TestCalendarEnterOpensTheEvent(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	if m = press(m, "enter"); m.month.focus != focusAgenda {
		t.Fatal("enter on the grid did not move into the day pane")
	}
	// Aug 28 reads: the all-day trip, Standup, then the 1:1.
	m = press(m, "down", "down", "enter")
	if m.overlay != overlayEvent {
		t.Fatalf("enter on the day pane left the overlay at %v", m.overlay)
	}
	out := stripANSI(m.View())
	for _, want := range []string{
		"1:1 with a very long meeting title",
		"2026-08-28 (Fri)  14:00–15:00",
		"Meeting room B",
		"- last week",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the event overlay is missing %q\n%s", want, out)
		}
	}
	// The first esc closes the overlay, the second leaves the day pane.
	if m = press(m, "esc"); m.overlay != overlayNone || m.month.focus != focusAgenda {
		t.Errorf("esc left overlay %v, focus %v", m.overlay, m.month.focus)
	}
	if m = press(m, "esc"); m.month.focus != focusGrid {
		t.Error("esc did not hand the focus back to the grid")
	}
}

// TestEventDetailShowsTheReplies is the answer to "am I going to this?": the
// overlay must say what the owner replied, and what everyone else did.
func TestEventDetailShowsTheReplies(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	m = press(m, "enter", "down", "down", "enter")
	if m.overlay != overlayEvent {
		t.Fatalf("the overlay is %v", m.overlay)
	}
	out := stripANSI(m.View())
	for _, want := range []string{
		"You        ? maybe", // the owner's own PARTSTAT
		"3 guests  ·  1 going, 1 maybe, 1 not going",
		"✓ Alice  (organizer)",
		"? Me  (you)",
		"✗ Bob",
		"Rooms      Room B", // resources are not guests
		"Repeats    every week on Fri",
		"Call       https://meet.example.invalid/abc-defg-hij",
		"o open the link",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the event overlay is missing %q\n%s", want, out)
		}
	}
}

// TestEventDetailStaysQuietWithoutGuests keeps the reply lines out of an
// event that nobody was invited to, where the feed says nothing about replies.
func TestEventDetailStaysQuietWithoutGuests(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	m = press(m, "enter", "down", "enter") // Standup: no attendees in the fixture
	out := stripANSI(m.View())
	for _, unwanted := range []string{"You  ", "guest", "no reply", "Rooms"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the overlay invented %q for an event with no guests\n%s", unwanted, out)
		}
	}
}

// TestCalendarArrowsFollowTheFocus is the point of the two panes: the same
// keys step between days in the grid and between entries in the day pane.
func TestCalendarArrowsFollowTheFocus(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	day := m.month.day

	m = press(m, "enter", "down")
	if !m.month.day.Equal(day) {
		t.Errorf("down in the day pane moved the day to %s", m.month.day.Format("2006-01-02"))
	}
	if m.month.agenda != 1 {
		t.Errorf("down in the day pane left the cursor at %d, want 1", m.month.agenda)
	}
	// Aug 28 holds three entries, so the cursor stops at the last one.
	if m = press(m, "down", "down", "down"); m.month.agenda != 2 {
		t.Errorf("the day pane cursor ran to %d, want it pinned at 2", m.month.agenda)
	}

	m = press(m, "esc", "down")
	if m.month.focus != focusGrid {
		t.Fatal("esc did not hand the focus back to the grid")
	}
	if want := day.AddDate(0, 0, 7); !m.month.day.Equal(want) {
		t.Errorf("down in the grid selected %s, want %s",
			m.month.day.Format("2006-01-02"), want.Format("2006-01-02"))
	}
	if m.month.agenda != 0 {
		t.Errorf("moving to another day left the pane cursor at %d", m.month.agenda)
	}
}

// TestCalendarEnterOnAnEmptyDayDoesNothing keeps the overlay shut when the
// cursor has nothing under it.
func TestCalendarEnterOnAnEmptyDayDoesNothing(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	m.month.day = time.Date(2026, 8, 5, 0, 0, 0, 0, time.Local)
	if m = press(m, "enter", "enter"); m.overlay != overlayNone {
		t.Errorf("enter on an empty day opened overlay %v", m.overlay)
	}
	if m.month.focus != focusGrid {
		t.Error("an empty day should not take the focus into the day pane")
	}
}

// TestEventWhenSpellsOutTheSpan pins the all-day arithmetic: iCalendar's end
// date is exclusive, so a trip ending Aug 30 covers through Aug 29.
func TestEventWhenSpellsOutTheSpan(t *testing.T) {
	events := testEvents()
	cases := map[int]string{
		0: "2026-08-27 (Thu) – 2026-08-29 (Sat)  all-day",
		1: "2026-08-28 (Fri)  10:00–10:30",
	}
	for i, want := range cases {
		if got := eventWhen(events[i]); got != want {
			t.Errorf("eventWhen(%q) = %q, want %q", events[i].Summary, got, want)
		}
	}
}

// TestCalendarShowsNoTasks checks the rendered frame rather than the model:
// no task from the project may reach the month grid or the day pane.
func TestCalendarShowsNoTasks(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	out := stripANSI(m.View())
	// #102 is due Aug 27, #103 runs Aug 26 – Sep 10 and the draft is due Aug 31,
	// so every one of them falls inside the rendered August grid.
	for _, task := range []string{"請求書の確認", "quarterly report", "Draft with no issue"} {
		if strings.Contains(out, task) {
			t.Errorf("the calendar shows the task %q", task)
		}
	}
	if !strings.Contains(out, "Standup") {
		t.Error("the calendar dropped its events along with the tasks")
	}
}

// TestRoadmapHidesDoneTasks keeps finished work off the timeline, and keeps
// the fact that it exists in the legend rather than dropping it in silence.
func TestRoadmapHidesDoneTasks(t *testing.T) {
	m := newTestModel(120, 40)
	// #104 is scheduled for Aug 14 and its status is Done.
	for _, it := range m.roadmap.items {
		if it.Number == 104 {
			t.Error("a task whose status is Done is still on the roadmap")
		}
	}
	if m.roadmap.doneHidden != 1 {
		t.Errorf("doneHidden = %d, want 1", m.roadmap.doneHidden)
	}
	m.view = viewRoadmap
	if out := stripANSI(m.View()); !strings.Contains(out, "1 done hidden") {
		t.Errorf("the legend does not mention the hidden task\n%s", out)
	}

	// A closed issue that nobody moved out of its column is still live work on
	// the board, so the roadmap keeps showing it.
	for i := range m.project.Items {
		if m.project.Items[i].Number == 103 { // In Progress, Aug 26 – Sep 10
			m.project.Items[i].State = "CLOSED"
		}
	}
	m.rebuild()
	var found bool
	for _, it := range m.roadmap.items {
		if it.Number == 103 {
			found = true
		}
	}
	if !found {
		t.Error("a closed issue whose status is not Done was dropped from the roadmap")
	}
}

// TestRoadmapSaysWhenEverythingIsDone keeps the empty view from blaming the
// dates when the real reason is that the work is finished.
func TestRoadmapSaysWhenEverythingIsDone(t *testing.T) {
	m := newTestModel(120, 40)
	for i := range m.project.Items {
		m.project.Items[i].Status = "Done"
	}
	m.rebuild()
	m.view = viewRoadmap
	out := stripANSI(m.View())
	if !strings.Contains(out, "every scheduled task is Done") {
		t.Errorf("the empty roadmap does not say why it is empty\n%s", out)
	}
}

// TestRoadmapOpensOnLiveWork guards the cursor default: with a backlog of
// finished tasks the roadmap should not open scrolled to last month.
func TestRoadmapOpensOnLiveWork(t *testing.T) {
	m := newTestModel(120, 40)
	it := m.roadmap.items[m.roadmap.cursor]
	if _, end := it.Span(); end.Before(startOfDay(testNow)) {
		t.Errorf("roadmap opened on %q, which ended %s (before today)", it.Title, end.Format("2006-01-02"))
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		tm, _ := m.Update(key(k))
		m = tm.(Model)
	}
	return m
}

func TestBracketKeysSwitchViews(t *testing.T) {
	m := newTestModel(120, 40)
	if m.view != viewBoard {
		t.Fatalf("start view = %v", m.view)
	}
	for _, want := range []viewID{viewRoadmap, viewCalendar, viewBoard} {
		m = press(m, "]")
		if m.view != want {
			t.Fatalf("after ]: view = %v, want %v", m.view, want)
		}
	}
	for _, want := range []viewID{viewCalendar, viewRoadmap, viewBoard} {
		m = press(m, "[")
		if m.view != want {
			t.Fatalf("after [: view = %v, want %v", m.view, want)
		}
	}
}

// TestMovementIsArrowsOnly locks in the rebinding: h/j/k/l must be inert in
// every view, and the arrows must do the moving.
func TestMovementIsArrowsOnly(t *testing.T) {
	t.Run("board", func(t *testing.T) {
		m := newTestModel(120, 40)
		before := m.board.col
		m = press(m, "l", "l", "j", "k", "h")
		if m.board.col != before {
			t.Errorf("hjkl moved the board cursor to col %d, want %d", m.board.col, before)
		}
		if m.view != viewBoard {
			t.Errorf("hjkl changed the view to %v", m.view)
		}
		m = press(m, "right")
		if m.board.col != before+1 {
			t.Errorf("right arrow did not move the column: %d", m.board.col)
		}
		m = press(m, "down")
		if m.board.row != 1 {
			t.Errorf("down arrow did not move the row: %d", m.board.row)
		}
	})

	t.Run("roadmap", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.view = viewRoadmap
		origin, cursor := m.roadmap.origin, m.roadmap.cursor
		m = press(m, "h", "j", "k", "l")
		if !m.roadmap.origin.Equal(origin) || m.roadmap.cursor != cursor {
			t.Error("hjkl moved the roadmap")
		}
		m = press(m, "right")
		if !m.roadmap.origin.After(origin) {
			t.Error("right arrow did not scroll the timeline")
		}
		m = press(m, "down")
		if m.roadmap.cursor != cursor+1 {
			t.Errorf("down arrow did not move the selection: %d", m.roadmap.cursor)
		}
	})

	t.Run("calendar", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.view = viewCalendar
		day := m.month.day
		m = press(m, "h", "j", "k", "l")
		if !m.month.day.Equal(day) {
			t.Errorf("hjkl moved the selected day to %v", m.month.day)
		}
		m = press(m, "right")
		if !m.month.day.Equal(day.AddDate(0, 0, 1)) {
			t.Errorf("right arrow = %v, want %v", m.month.day, day.AddDate(0, 0, 1))
		}
		m = press(m, "down")
		if !m.month.day.Equal(day.AddDate(0, 0, 8)) {
			t.Errorf("down arrow = %v, want +8 days", m.month.day)
		}
	})
}

func TestRoadmapZoomMovedToPlusMinus(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewRoadmap
	zoom := m.roadmap.zoom
	m = press(m, "-")
	if m.roadmap.zoom != zoom+1 {
		t.Errorf("- did not zoom out: %d", m.roadmap.zoom)
	}
	if m.view != viewRoadmap {
		t.Errorf("- changed the view to %v", m.view)
	}
	m = press(m, "+")
	if m.roadmap.zoom != zoom {
		t.Errorf("+ did not zoom back in: %d", m.roadmap.zoom)
	}
	// [ and ] must switch views here, not zoom.
	m = press(m, "]")
	if m.view != viewCalendar {
		t.Errorf("] in the roadmap = view %v, want Calendar", m.view)
	}
}

// TestFormSwallowsViewKeys guards the caveat that the registration form takes
// bracket keys as text rather than switching views underneath it.
func TestFormSwallowsViewKeys(t *testing.T) {
	m := newTestModel(120, 40)
	tm, _ := m.openForm()
	m = tm.(Model)
	m = press(m, "[", "]", "1")
	if m.view != viewBoard {
		t.Errorf("the form leaked keys to the view switch: %v", m.view)
	}
	if got := m.form.title.Value(); got != "[]1" {
		t.Errorf("form title = %q, want %q", got, "[]1")
	}
	m = press(m, "esc")
	if m.overlay != overlayNone {
		t.Error("esc did not close the form")
	}
	m = press(m, "]")
	if m.view != viewRoadmap {
		t.Errorf("after esc, ] should switch views; view = %v", m.view)
	}
}

func wheel(button tea.MouseButton, shift bool) tea.MouseMsg {
	return tea.MouseMsg(tea.MouseEvent{
		Action: tea.MouseActionPress,
		Button: button,
		Shift:  shift,
	})
}

// scroll sends each event enough times to cross the sensitivity threshold, so
// the tests below read as "one gesture per assertion".
func scroll(m Model, events ...tea.MouseMsg) Model {
	for _, e := range events {
		for i := 0; i < m.cfg.UI.TicksPerStep(); i++ {
			tm, _ := m.Update(e)
			m = tm.(Model)
		}
	}
	return m
}

// scrollOnce sends a single wheel event, without completing a step.
func scrollOnce(m Model, e tea.MouseMsg) Model {
	tm, _ := m.Update(e)
	return tm.(Model)
}

func TestTrackpadScrollMovesInEveryDirection(t *testing.T) {
	t.Run("board", func(t *testing.T) {
		m := newTestModel(120, 40)
		m = scroll(m, wheel(tea.MouseButtonWheelDown, false))
		if m.board.row != 1 {
			t.Errorf("scroll down: row = %d, want 1", m.board.row)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelUp, false))
		if m.board.row != 0 {
			t.Errorf("scroll up: row = %d, want 0", m.board.row)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelRight, false))
		if m.board.col != 1 {
			t.Errorf("scroll right: col = %d, want 1", m.board.col)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelLeft, false))
		if m.board.col != 0 {
			t.Errorf("scroll left: col = %d, want 0", m.board.col)
		}
	})

	t.Run("shift is the horizontal fallback", func(t *testing.T) {
		m := newTestModel(120, 40)
		m = scroll(m, wheel(tea.MouseButtonWheelDown, true))
		if m.board.col != 1 {
			t.Errorf("shift+scroll down: col = %d, want 1", m.board.col)
		}
		if m.board.row != 0 {
			t.Errorf("shift+scroll down moved the row to %d", m.board.row)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelUp, true))
		if m.board.col != 0 {
			t.Errorf("shift+scroll up: col = %d, want 0", m.board.col)
		}
	})

	t.Run("calendar", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.view = viewCalendar
		day := m.month.day
		m = scroll(m, wheel(tea.MouseButtonWheelDown, false))
		if !m.month.day.Equal(day.AddDate(0, 0, 7)) {
			t.Errorf("scroll down = %v, want one week later", m.month.day)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelRight, false))
		if !m.month.day.Equal(day.AddDate(0, 0, 8)) {
			t.Errorf("scroll right = %v, want one more day", m.month.day)
		}
	})

	t.Run("roadmap", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.view = viewRoadmap
		origin, cursor := m.roadmap.origin, m.roadmap.cursor
		m = scroll(m, wheel(tea.MouseButtonWheelDown, false))
		if m.roadmap.cursor != cursor+1 {
			t.Errorf("scroll down: cursor = %d, want %d", m.roadmap.cursor, cursor+1)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelRight, false))
		if !m.roadmap.origin.After(origin) {
			t.Errorf("scroll right did not advance the timeline")
		}
	})

	t.Run("detail overlay", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.overlay = overlayDetail
		m.detail = detailState{item: m.project.Items[2]}
		m = scroll(m, wheel(tea.MouseButtonWheelDown, false), wheel(tea.MouseButtonWheelDown, false))
		if m.detail.scroll != 2 {
			t.Errorf("detail scroll = %d, want 2", m.detail.scroll)
		}
		m = scroll(m, wheel(tea.MouseButtonWheelUp, false))
		if m.detail.scroll != 1 {
			t.Errorf("detail scroll = %d, want 1", m.detail.scroll)
		}
	})
}

// TestNonWheelMouseEventsAreIgnored keeps clicks and bare pointer motion from
// being mistaken for navigation.
func TestNonWheelMouseEventsAreIgnored(t *testing.T) {
	m := newTestModel(120, 40)
	before := m.board
	for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight, tea.MouseButtonNone} {
		m = scroll(m, wheel(button, false))
	}
	m = scroll(m, tea.MouseMsg(tea.MouseEvent{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}))
	if m.board.col != before.col || m.board.row != before.row {
		t.Errorf("a non-wheel event moved the cursor to col %d row %d", m.board.col, m.board.row)
	}
}

// TestScrollSensitivity covers the accumulator: a trackpad burst has to reach
// the configured tick count before the cursor moves at all.
func TestScrollSensitivity(t *testing.T) {
	ticks := config.DefaultScrollTicks

	t.Run("partial gestures do not move", func(t *testing.T) {
		m := newTestModel(120, 40)
		for i := 0; i < ticks-1; i++ {
			m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
			if m.board.row != 0 {
				t.Fatalf("moved after %d of %d ticks", i+1, ticks)
			}
		}
		m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		if m.board.row != 1 {
			t.Errorf("did not move on tick %d: row = %d", ticks, m.board.row)
		}
	})

	t.Run("the accumulator resets after a step", func(t *testing.T) {
		m := newTestModel(120, 40)
		m = scroll(m, wheel(tea.MouseButtonWheelDown, false))
		if m.scroll.vertical != 0 {
			t.Errorf("accumulator = %d after a completed step, want 0", m.scroll.vertical)
		}
		// A second full gesture must take the same number of ticks again.
		for i := 0; i < ticks-1; i++ {
			m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		}
		if m.board.row != 1 {
			t.Errorf("second gesture moved early: row = %d", m.board.row)
		}
	})

	t.Run("reversing direction abandons the partial count", func(t *testing.T) {
		m := newTestModel(120, 40)
		for i := 0; i < ticks-1; i++ {
			m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		}
		// Flicking back the other way should not immediately complete a step.
		m = scrollOnce(m, wheel(tea.MouseButtonWheelUp, false))
		if m.scroll.vertical != -1 {
			t.Errorf("accumulator = %d after reversing, want -1", m.scroll.vertical)
		}
		if m.board.row != 0 {
			t.Errorf("row = %d, want 0", m.board.row)
		}
	})

	t.Run("changing axis abandons the other partial count", func(t *testing.T) {
		m := newTestModel(120, 40)
		for i := 0; i < ticks-1; i++ {
			m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		}
		m = scrollOnce(m, wheel(tea.MouseButtonWheelRight, false))
		if m.scroll.vertical != 0 {
			t.Errorf("vertical accumulator = %d, want 0", m.scroll.vertical)
		}
		if m.scroll.horizontal != 1 {
			t.Errorf("horizontal accumulator = %d, want 1", m.scroll.horizontal)
		}
	})

	t.Run("scroll_ticks = 1 moves on every event", func(t *testing.T) {
		m := newTestModel(120, 40)
		m.cfg.UI.ScrollTicks = 1
		m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		if m.board.row != 1 {
			t.Errorf("row = %d, want 1", m.board.row)
		}
	})
}

// TestScrollCooldownTamesMomentum covers the second limiter: however many wheel
// events a trackpad's momentum emits, steps cannot come faster than the
// configured interval.
func TestScrollCooldownTamesMomentum(t *testing.T) {
	fixed := testNow
	m := newTestModel(120, 40)
	m.view = viewCalendar // stepping by weeks is unbounded, unlike a board column
	m.clock = func() time.Time { return fixed }
	start := m.month.day

	gesture := func() {
		for i := 0; i < config.DefaultScrollTicks; i++ {
			m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
		}
	}

	// A full gesture's worth of ticks produces exactly one step.
	gesture()
	if want := start.AddDate(0, 0, 7); !m.month.day.Equal(want) {
		t.Fatalf("first step: day = %v, want %v", m.month.day, want)
	}

	// A momentum tail of 200 more events, all inside the cooldown, must not move.
	for i := 0; i < 200; i++ {
		m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
	}
	if want := start.AddDate(0, 0, 7); !m.month.day.Equal(want) {
		t.Errorf("momentum moved the day to %v, want %v", m.month.day, want)
	}
	if m.scroll.vertical != 0 {
		t.Errorf("momentum banked %d ticks; they should be discarded", m.scroll.vertical)
	}

	// Once the cooldown lapses, scrolling works again.
	fixed = fixed.Add(config.DefaultScrollIntervalMS * time.Millisecond)
	gesture()
	if want := start.AddDate(0, 0, 14); !m.month.day.Equal(want) {
		t.Errorf("after the cooldown: day = %v, want %v", m.month.day, want)
	}
}

func TestScrollIntervalConfig(t *testing.T) {
	// Absent means the default.
	if got := (config.UI{}).ScrollInterval(); got != config.DefaultScrollIntervalMS*time.Millisecond {
		t.Errorf("default interval = %v", got)
	}
	// An explicit zero removes the cap.
	zero := 0
	if got := (config.UI{ScrollIntervalMS: &zero}).ScrollInterval(); got != 0 {
		t.Errorf("interval = %v, want 0", got)
	}
	ms := 400
	if got := (config.UI{ScrollIntervalMS: &ms}).ScrollInterval(); got != 400*time.Millisecond {
		t.Errorf("interval = %v, want 400ms", got)
	}
	// A negative value is clamped rather than inverting the comparison.
	neg := -50
	if got := (config.UI{ScrollIntervalMS: &neg}).ScrollInterval(); got != 0 {
		t.Errorf("interval = %v, want 0", got)
	}
}

// TestScrollCapCanBeRemoved confirms the escape hatch for mouse-wheel users.
func TestScrollCapCanBeRemoved(t *testing.T) {
	fixed := testNow
	zero := 0
	m := newTestModel(120, 40)
	m.view = viewCalendar
	m.clock = func() time.Time { return fixed }
	m.cfg.UI.ScrollTicks = 1
	m.cfg.UI.ScrollIntervalMS = &zero
	start := m.month.day
	for i := 0; i < 3; i++ {
		m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
	}
	if want := start.AddDate(0, 0, 21); !m.month.day.Equal(want) {
		t.Errorf("day = %v, want %v with both limits off", m.month.day, want)
	}
}

func TestTicksPerStepGuardsBadConfig(t *testing.T) {
	for _, in := range []int{0, -1, -100} {
		if got := (config.UI{ScrollTicks: in}).TicksPerStep(); got != config.DefaultScrollTicks {
			t.Errorf("ScrollTicks=%d: TicksPerStep() = %d, want %d", in, got, config.DefaultScrollTicks)
		}
	}
	if got := (config.UI{ScrollTicks: 7}).TicksPerStep(); got != 7 {
		t.Errorf("TicksPerStep() = %d, want 7", got)
	}
}

func TestMouseCanBeDisabledInConfig(t *testing.T) {
	var ui config.UI
	if !ui.MouseEnabled() {
		t.Error("mouse should default to enabled when the key is absent")
	}
	off := false
	ui.Mouse = &off
	if ui.MouseEnabled() {
		t.Error("mouse = false should disable it")
	}
	on := true
	ui.Mouse = &on
	if !ui.MouseEnabled() {
		t.Error("mouse = true should enable it")
	}
}

// TestCacheGivesAFirstFrameWithContent is the point of the cache: a cold start
// paints real cards off disk instead of a loading screen, then the network
// response replaces them.
func TestCacheGivesAFirstFrameWithContent(t *testing.T) {
	t.Setenv("TOOLBOX_TUI_CACHE", t.TempDir())

	// First run: nothing cached, so the first frame is the loading screen.
	first := newTestModel(120, 40)
	first.project = nil
	first.events = nil
	if msg := first.loadCache()(); msg.(cacheMsg).project != nil {
		t.Fatal("nothing should be cached yet")
	}
	if out := stripANSI(first.View()); !strings.Contains(out, "loading project") {
		t.Errorf("cold first frame should say it is loading:\n%s", out)
	}

	// The network response lands and is persisted.
	tm, cmd := first.Update(projectMsg{project: testProject()})
	first = tm.(Model)
	if cmd == nil {
		t.Fatal("a successful fetch should persist the project")
	}
	cmd() // run the batched save
	tm, _ = first.Update(eventsMsg{events: testEvents()})
	first = tm.(Model)
	saveCache(cacheEvents, first.events)()

	// Second run: the cache fills the first frame before any request completes.
	second := newTestModel(120, 40)
	second.project = nil
	second.events = nil
	second.rebuild()
	msg := second.loadCache()()
	tm, _ = second.Update(msg)
	second = tm.(Model)

	if second.project == nil {
		t.Fatal("the cache did not restore the project")
	}
	if got := len(second.project.Items); got != len(testProject().Items) {
		t.Errorf("restored %d items, want %d", got, len(testProject().Items))
	}
	if len(second.events) != len(testEvents()) {
		t.Errorf("restored %d events, want %d", len(second.events), len(testEvents()))
	}
	if !second.projectStale || !second.eventsStale {
		t.Error("cache-sourced data should be marked stale until the network confirms it")
	}
	out := stripANSI(second.View())
	if strings.Contains(out, "loading project") {
		t.Errorf("cached first frame should show cards:\n%s", out)
	}
	if !strings.Contains(out, "#101") {
		t.Errorf("cached first frame is missing a known card:\n%s", out)
	}

	// Dates and labels have to survive the JSON round trip, or the board lies.
	var restored *gh.Item
	for i := range second.project.Items {
		if second.project.Items[i].ID == "i1" {
			restored = &second.project.Items[i]
		}
	}
	if restored == nil {
		t.Fatal("item i1 missing after the round trip")
	}
	if restored.EndDate == nil || !restored.EndDate.Equal(*day(2026, 8, 14)) {
		t.Errorf("EndDate = %v, want 2026-08-14", restored.EndDate)
	}
	if len(restored.Labels) != 1 || restored.Labels[0].Name != "Develop" {
		t.Errorf("labels = %+v", restored.Labels)
	}

	// A fresh network response clears the stale marks.
	tm, _ = second.Update(projectMsg{project: testProject()})
	second = tm.(Model)
	if second.projectStale {
		t.Error("projectStale should clear once the network responds")
	}
}

// TestNetworkResponseWinsOverLateCache covers the race: a cache read that
// arrives after the network must not overwrite fresh data.
func TestNetworkResponseWinsOverLateCache(t *testing.T) {
	m := newTestModel(120, 40)
	fresh := m.project
	stale := testProject()
	stale.Items = stale.Items[:1]

	tm, _ := m.Update(cacheMsg{project: stale, at: testNow.Add(-time.Hour)})
	m = tm.(Model)
	if len(m.project.Items) != len(fresh.Items) {
		t.Errorf("a late cache read clobbered fresh data: %d items", len(m.project.Items))
	}
	if m.projectStale {
		t.Error("projectStale should not be set when the cache was ignored")
	}
}

// TestFailedCalendarRefreshKeepsCachedEvents stops a transient feed error from
// blanking the calendar.
func TestFailedCalendarRefreshKeepsCachedEvents(t *testing.T) {
	m := newTestModel(120, 40)
	before := len(m.events)
	tm, _ := m.Update(eventsMsg{errs: []error{errors.New("timeout")}})
	m = tm.(Model)
	if len(m.events) != before {
		t.Errorf("events dropped to %d after a failed refresh, want %d kept", len(m.events), before)
	}
}

func TestHumanAge(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "just now",
		5 * time.Minute:  "5m ago",
		3 * time.Hour:    "3h ago",
		50 * time.Hour:   "2d ago",
		90 * time.Minute: "1h ago",
	}
	for d, want := range cases {
		if got := humanAge(d); got != want {
			t.Errorf("humanAge(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestAdjustScrollWalksThePresetLadder(t *testing.T) {
	m := newTestModel(120, 40)
	if m.scrollLevel != config.DefaultScrollLevel {
		t.Fatalf("start level = %d, want %d", m.scrollLevel, config.DefaultScrollLevel)
	}

	m = press(m, ".")
	if m.scrollLevel != config.DefaultScrollLevel+1 {
		t.Errorf("'.' level = %d, want %d", m.scrollLevel, config.DefaultScrollLevel+1)
	}
	want := config.ScrollPresets[m.scrollLevel]
	if m.cfg.UI.TicksPerStep() != want.Ticks {
		t.Errorf("ticks = %d, want %d", m.cfg.UI.TicksPerStep(), want.Ticks)
	}
	if got := int(m.cfg.UI.ScrollInterval().Milliseconds()); got != want.IntervalMS {
		t.Errorf("interval = %d, want %d", got, want.IntervalMS)
	}
	if !strings.Contains(m.status, "scroll_ticks") {
		t.Errorf("status should show the config line to keep: %q", m.status)
	}

	m = press(m, ",", ",")
	if m.scrollLevel != config.DefaultScrollLevel-1 {
		t.Errorf("after two ',': level = %d, want %d", m.scrollLevel, config.DefaultScrollLevel-1)
	}
}

func TestAdjustScrollClampsAtBothEnds(t *testing.T) {
	m := newTestModel(120, 40)
	for i := 0; i < 20; i++ {
		m = press(m, ",")
	}
	if m.scrollLevel != 0 {
		t.Errorf("level = %d, want 0 at the slow end", m.scrollLevel)
	}
	if !strings.Contains(m.status, "slowest") {
		t.Errorf("status = %q, want it to say slowest", m.status)
	}
	for i := 0; i < 20; i++ {
		m = press(m, ".")
	}
	if want := len(config.ScrollPresets) - 1; m.scrollLevel != want {
		t.Errorf("level = %d, want %d at the fast end", m.scrollLevel, want)
	}
	if !strings.Contains(m.status, "fastest") {
		t.Errorf("status = %q, want it to say fastest", m.status)
	}
	// The fastest rung removes both limits.
	if m.cfg.UI.ScrollInterval() != 0 || m.cfg.UI.TicksPerStep() != 1 {
		t.Errorf("fastest preset = ticks %d interval %v, want 1 and 0",
			m.cfg.UI.TicksPerStep(), m.cfg.UI.ScrollInterval())
	}
}

// TestAdjustScrollTakesEffectImmediately is the point of the live keys: the new
// setting must apply to the very next gesture, not after a restart.
func TestAdjustScrollTakesEffectImmediately(t *testing.T) {
	fixed := testNow
	m := newTestModel(120, 40)
	m.view = viewCalendar
	m.clock = func() time.Time { return fixed }

	// Jump to the fastest rung, where every event is a step.
	for i := 0; i < len(config.ScrollPresets); i++ {
		m = press(m, ".")
	}
	start := m.month.day
	for i := 0; i < 4; i++ {
		m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
	}
	if want := start.AddDate(0, 0, 28); !m.month.day.Equal(want) {
		t.Errorf("day = %v, want %v — the new sensitivity did not apply", m.month.day, want)
	}
}

func TestAdjustScrollDiscardsPartialCount(t *testing.T) {
	m := newTestModel(120, 40)
	m = scrollOnce(m, wheel(tea.MouseButtonWheelDown, false))
	if m.scroll.vertical == 0 {
		t.Fatal("expected a partial count to bank")
	}
	m = press(m, ",")
	if m.scroll.vertical != 0 || m.scroll.horizontal != 0 {
		t.Errorf("changing sensitivity left a partial count: %+v", m.scroll)
	}
}

func TestNearestScrollLevel(t *testing.T) {
	// The documented defaults must map back to the default rung.
	if got := config.NearestScrollLevel(config.DefaultScrollTicks, config.DefaultScrollIntervalMS); got != config.DefaultScrollLevel {
		t.Errorf("defaults map to level %d, want %d", got, config.DefaultScrollLevel)
	}
	// Every rung must round-trip to itself.
	for i, p := range config.ScrollPresets {
		if got := config.NearestScrollLevel(p.Ticks, p.IntervalMS); got != i {
			t.Errorf("preset %d (%+v) mapped to %d", i, p, got)
		}
	}
	// A hand-edited slow setting lands near the slow end, not on the default.
	if got := config.NearestScrollLevel(5, 150); got > config.DefaultScrollLevel {
		t.Errorf("ticks=5 interval=150 mapped to level %d, want a slower rung", got)
	}
}

func TestStepsPerSecond(t *testing.T) {
	if got := (config.ScrollPreset{IntervalMS: 100}).StepsPerSecond(); got != 10 {
		t.Errorf("= %d, want 10", got)
	}
	if got := (config.ScrollPreset{IntervalMS: 0}).StepsPerSecond(); got != 0 {
		t.Errorf("= %d, want 0 for uncapped", got)
	}
}

// findInFrame locates a substring in a rendered frame and returns its cell
// coordinates. Tests use it so click assertions are anchored to where the text
// really is, not to a second copy of the layout arithmetic.
func findInFrame(frame, needle string) (x, y int, ok bool) {
	for i, line := range strings.Split(frame, "\n") {
		plain := stripANSI(line)
		if idx := strings.Index(plain, needle); idx >= 0 {
			return ansi.StringWidth(plain[:idx]), i, true
		}
	}
	return 0, 0, false
}

func click(m Model, x, y int) Model {
	msg := tea.MouseMsg(tea.MouseEvent{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      x,
		Y:      y,
	})
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func TestClickSelectsBoardCard(t *testing.T) {
	m := newTestModel(120, 40)
	frame := m.View()

	// Every card the fixture puts on screen must be selectable by clicking its title.
	for _, want := range []struct{ needle, id string }{
		{"#105", "i5"},
		{"#103", "i3"},
		{"#102", "i2"},
		{"#104", "i4"},
		{"#107", "i7"},
	} {
		x, y, ok := findInFrame(frame, want.needle)
		if !ok {
			t.Errorf("%s is not on the frame", want.needle)
			continue
		}
		clicked := click(m, x, y)
		got, has := clicked.board.selected()
		if !has {
			t.Errorf("clicking %s at (%d,%d) selected nothing", want.needle, x, y)
			continue
		}
		if got.ID != want.id {
			t.Errorf("clicking %s at (%d,%d) selected %q (%s), want %s",
				want.needle, x, y, got.Title, got.ID, want.id)
		}
	}
}

func TestClickOnEmptyBoardAreaChangesNothing(t *testing.T) {
	m := newTestModel(120, 40)
	_ = m.View()
	before := m.board
	// Well below the last card in the first column.
	m = click(m, 3, 38)
	if m.board.col != before.col || m.board.row != before.row {
		t.Errorf("a click on empty space moved the cursor to col %d row %d", m.board.col, m.board.row)
	}
}

func TestClickSelectsRoadmapRow(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewRoadmap
	frame := m.View()

	for i, item := range m.roadmap.items {
		needle := item.Title
		if item.Number > 0 {
			needle = fmt.Sprintf("#%d", item.Number)
		}
		x, y, ok := findInFrame(frame, needle)
		if !ok {
			continue // scrolled out of the window
		}
		clicked := click(m, x, y)
		if clicked.roadmap.cursor != i {
			t.Errorf("clicking %q at (%d,%d) selected row %d, want %d",
				needle, x, y, clicked.roadmap.cursor, i)
		}
	}
}

// TestClickSelectsCalendarDay checks every cell of the month grid: the day
// number printed in a cell must be the day that clicking it selects. This
// compares the hit map against the rendered text rather than against the same
// arithmetic that produced it.
func TestClickSelectsCalendarDay(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	lines := strings.Split(m.View(), "\n")

	var checked int
	for _, r := range m.hits.regions {
		if r.kind != hitCalendarDay {
			continue
		}
		if r.y >= len(lines) {
			t.Fatalf("region for %s is at row %d, past the %d-line frame",
				r.day.Format("2006-01-02"), r.y, len(lines))
		}
		// A cell's first line holds only its day number, and the column rules
		// beside it are single-width box characters, so runes are cells here.
		plain := []rune(stripANSI(lines[r.y]))
		if r.x+r.w > len(plain) {
			t.Fatalf("region for %s spans past the line", r.day.Format("2006-01-02"))
		}
		cell := string(plain[r.x : r.x+r.w])
		if got := strings.TrimSpace(cell); got != fmt.Sprint(r.day.Day()) &&
			got != "["+fmt.Sprint(r.day.Day())+"]" {
			t.Errorf("cell at (%d,%d) reads %q but is mapped to %s",
				r.x, r.y, got, r.day.Format("2006-01-02"))
			continue
		}
		// Clicking the middle of the cell must select exactly that day.
		clicked := click(m, r.x+r.w/2, r.y+r.h/2)
		if !clicked.month.day.Equal(r.day) {
			t.Errorf("clicking cell %q selected %s, want %s",
				strings.TrimSpace(cell), clicked.month.day.Format("2006-01-02"), r.day.Format("2006-01-02"))
		}
		checked++
	}
	if want := calendarWeeks * 7; checked != want {
		t.Errorf("checked %d cells, want %d", checked, want)
	}
}

// TestClickSelectsOutOfMonthDay confirms the leading and trailing days of the
// grid navigate to their real month rather than snapping inside the current one.
func TestClickSelectsOutOfMonthDay(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	_ = m.View()

	var july, september bool
	for _, r := range m.hits.regions {
		if r.kind != hitCalendarDay {
			continue
		}
		clicked := click(m, r.x+r.w/2, r.y+r.h/2)
		if !clicked.month.day.Equal(r.day) {
			t.Fatalf("cell %s selected %s", r.day.Format("2006-01-02"), clicked.month.day.Format("2006-01-02"))
		}
		switch r.day.Month() {
		case time.July:
			july = true
		case time.September:
			september = true
		}
	}
	if !july || !september {
		t.Errorf("the August grid should show July (%v) and September (%v) days", july, september)
	}
}

func TestClickSelectsAgendaEntry(t *testing.T) {
	m := newTestModel(120, 40)
	m.view = viewCalendar
	frame := m.View()

	// The agenda for Aug 28 lists the all-day trip and the two timed events.
	entries := m.agenda(m.month.day)
	if len(entries) < 3 {
		t.Fatalf("fixture agenda has %d entries", len(entries))
	}
	x, y, ok := findInFrame(frame, "Standup")
	if !ok {
		t.Fatal("Standup is not on the frame")
	}
	clicked := click(m, x, y)
	want := -1
	for i, e := range entries {
		if e.Summary == "Standup" {
			want = i
		}
	}
	if clicked.month.agenda != want {
		t.Errorf("clicking Standup at (%d,%d) selected agenda %d, want %d", x, y, clicked.month.agenda, want)
	}
}

func TestClicksAreInertWhileAnOverlayIsOpen(t *testing.T) {
	m := newTestModel(120, 40)
	frame := m.View()
	x, y, ok := findInFrame(frame, "#105")
	if !ok {
		t.Fatal("#105 is not on the frame")
	}
	before := m.board

	for _, overlay := range []overlayID{overlayHelp, overlayDetail} {
		withOverlay := m
		withOverlay.overlay = overlay
		withOverlay.detail = detailState{item: m.project.Items[0]}
		_ = withOverlay.View() // the overlay frame clears the hit map
		withOverlay = click(withOverlay, x, y)
		if withOverlay.board.col != before.col || withOverlay.board.row != before.row {
			t.Errorf("overlay %v let a click through to the board", overlay)
		}
		if withOverlay.overlay != overlay {
			t.Errorf("a click closed overlay %v", overlay)
		}
	}
}

// TestClickDoesNotFeedTheScrollAccumulator keeps a click from counting as a
// scroll tick.
func TestClickDoesNotFeedTheScrollAccumulator(t *testing.T) {
	m := newTestModel(120, 40)
	_ = m.View()
	for i := 0; i < 10; i++ {
		m = click(m, 3, 4)
	}
	if m.scroll.vertical != 0 || m.scroll.horizontal != 0 {
		t.Errorf("clicks banked scroll ticks: %+v", m.scroll)
	}
}
