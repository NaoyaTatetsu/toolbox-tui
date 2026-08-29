package github

import (
	"testing"
	"time"
)

var now = time.Date(2026, 8, 28, 10, 0, 0, 0, time.Local)

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	return &t
}

func TestSpanCoversASingleDateAndSurvivesReversedOnes(t *testing.T) {
	cases := []struct {
		name             string
		start, end       *time.Time
		wantFrom, wantTo *time.Time
	}{
		{"both", day(2026, 8, 3), day(2026, 8, 14), day(2026, 8, 3), day(2026, 8, 14)},
		// A start after its end is a typo on the board, not a reason to draw a
		// bar backwards.
		{"reversed", day(2026, 8, 14), day(2026, 8, 3), day(2026, 8, 3), day(2026, 8, 14)},
		{"start only", day(2026, 8, 3), nil, day(2026, 8, 3), day(2026, 8, 3)},
		{"end only", nil, day(2026, 8, 3), day(2026, 8, 3), day(2026, 8, 3)},
	}
	for _, c := range cases {
		from, to := Item{StartDate: c.start, EndDate: c.end}.Span()
		if !from.Equal(*c.wantFrom) || !to.Equal(*c.wantTo) {
			t.Errorf("%s: Span() = %s–%s, want %s–%s", c.name,
				from.Format("01-02"), to.Format("01-02"),
				c.wantFrom.Format("01-02"), c.wantTo.Format("01-02"))
		}
	}
	if from, to := (Item{}).Span(); !from.IsZero() || !to.IsZero() {
		t.Errorf("an item with no dates spans %s–%s, want the zero time", from, to)
	}
}

func TestIsDoneReadsBothTheColumnAndTheIssue(t *testing.T) {
	cases := map[string]struct {
		item Item
		want bool
	}{
		"status Done":        {Item{Status: "Done", State: "OPEN"}, true},
		"status done, lower": {Item{Status: "done"}, true},
		"closed issue":       {Item{Status: "In Progress", State: "CLOSED"}, true},
		"merged pull":        {Item{Status: "In Review", State: "MERGED"}, true},
		"open and in flight": {Item{Status: "In Progress", State: "OPEN"}, false},
		"no status at all":   {Item{}, false},
	}
	for name, c := range cases {
		if got := c.item.IsDone(); got != c.want {
			t.Errorf("%s: IsDone() = %v, want %v", name, got, c.want)
		}
	}
}

// TestOverdueLeavesFinishedWorkAlone is the rule the board's red text depends
// on: a task that is finished cannot be late, however old its due date.
func TestOverdueLeavesFinishedWorkAlone(t *testing.T) {
	cases := map[string]struct {
		item Item
		want bool
	}{
		"past due, open":     {Item{EndDate: day(2026, 8, 27), State: "OPEN"}, true},
		"due today":          {Item{EndDate: day(2026, 8, 28), State: "OPEN"}, false},
		"due tomorrow":       {Item{EndDate: day(2026, 8, 29), State: "OPEN"}, false},
		"past due, done":     {Item{EndDate: day(2026, 8, 27), Status: "Done"}, false},
		"past due, closed":   {Item{EndDate: day(2026, 8, 27), State: "CLOSED"}, false},
		"no due date at all": {Item{State: "OPEN"}, false},
	}
	for name, c := range cases {
		if got := c.item.Overdue(now); got != c.want {
			t.Errorf("%s: Overdue() = %v, want %v", name, got, c.want)
		}
	}
}

func TestPriorityRankOrdersUrgentFirstAndUnsetLast(t *testing.T) {
	ranks := map[string]int{
		"High": 0, "urgent": 0, "P0": 0,
		"Middle": 1, "medium": 1, "p1": 1,
		"Low": 2, "p2": 2,
		"Blocker": 3, // unknown values sort after the ones we know
		"":        4,
	}
	for value, want := range ranks {
		if got := (Item{Priority: value}).PriorityRank(); got != want {
			t.Errorf("PriorityRank(%q) = %d, want %d", value, got, want)
		}
	}
}

// TestSortItemsTriageOrder pins the order a person triages in: what is late,
// then what is due soonest, then what matters most, then alphabetically.
func TestSortItemsTriageOrder(t *testing.T) {
	items := []Item{
		{Title: "no dates, high", Priority: "High"},
		{Title: "due later", EndDate: day(2026, 9, 10)},
		{Title: "late", EndDate: day(2026, 8, 20)},
		{Title: "due soon, low", EndDate: day(2026, 8, 29), Priority: "Low"},
		{Title: "due soon, high", EndDate: day(2026, 8, 29), Priority: "High"},
		{Title: "another late one", EndDate: day(2026, 8, 21)},
	}
	SortItems(items, now)

	want := []string{
		"late", // both are overdue; the older due date comes first
		"another late one",
		"due soon, high",
		"due soon, low",
		"due later",
		"no dates, high",
	}
	for i, w := range want {
		if items[i].Title != w {
			var got []string
			for _, it := range items {
				got = append(got, it.Title)
			}
			t.Fatalf("SortItems order = %q, want %q", got, want)
		}
	}
}

func testProject() *Project {
	return &Project{
		ID:    "P_1",
		Title: "Tasks",
		Fields: []Field{
			{ID: "f1", Name: "Status", DataType: FieldSingleSelect, Options: []Option{
				{ID: "s1", Name: "Todo", Color: "GREEN"},
				{ID: "s2", Name: "In Progress", Color: "YELLOW"},
				{ID: "s3", Name: "Done", Color: "PURPLE"},
			}},
			{ID: "f2", Name: "Priority", DataType: FieldSingleSelect, Options: []Option{
				{ID: "p1", Name: "High", Color: "RED"},
			}},
			{ID: "f3", Name: "Start Date", DataType: FieldDate},
			{ID: "f4", Name: "End Date", DataType: FieldDate},
		},
		Items: []Item{
			{ID: "i1", Title: "a todo", Status: "Todo"},
			{ID: "i2", Title: "in flight", Status: "In Progress", StartDate: day(2026, 8, 26), EndDate: day(2026, 9, 10)},
			{ID: "i3", Title: "finished", Status: "Done", EndDate: day(2026, 8, 14)},
			{ID: "i4", Title: "no status yet"},
			{ID: "i5", Title: "left over", Status: "Icebox"}, // a status the field no longer offers
		},
	}
}

func TestBoardFollowsThePreferredOrderAndKeepsStrayStatuses(t *testing.T) {
	p := testProject()
	cols := p.Board([]string{"In Progress", "Todo"}, false, now)

	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
	}
	want := []string{"In Progress", "Todo", "Done", "Icebox", "No Status"}
	if len(names) != len(want) {
		t.Fatalf("columns = %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("columns = %q, want %q", names, want)
		}
	}
	// A status the project no longer offers still has to show its card, or the
	// card would vanish from the board entirely.
	if len(cols[3].Items) != 1 || cols[3].Items[0].ID != "i5" {
		t.Errorf("the Icebox column holds %d items, want the stray card", len(cols[3].Items))
	}
	if cols[3].Color != "GRAY" {
		t.Errorf("stray column colour = %q, want GRAY", cols[3].Color)
	}
	if len(cols[4].Items) != 1 || cols[4].Items[0].ID != "i4" {
		t.Errorf("No Status holds %d items, want the card with no status", len(cols[4].Items))
	}
	if cols[0].Color != "YELLOW" {
		t.Errorf("In Progress colour = %q, want the project's own", cols[0].Color)
	}
}

func TestBoardHideDoneDropsOnlyTheDoneColumn(t *testing.T) {
	p := testProject()
	cols := p.Board(nil, true, now)
	for _, c := range cols {
		if c.Name == "Done" {
			t.Error("hideDone left the Done column on the board")
		}
	}
	if len(cols) != 4 { // Todo, In Progress, Icebox, No Status
		t.Errorf("got %d columns, want 4", len(cols))
	}
}

// TestBoardWithoutAStatusField still has to produce a board, since the Status
// field is a convention rather than something GitHub guarantees.
func TestBoardWithoutAStatusField(t *testing.T) {
	p := &Project{Items: []Item{{ID: "i1", Title: "orphan"}}}
	cols := p.Board(nil, false, now)
	if len(cols) != 1 || cols[0].Name != "No Status" {
		t.Fatalf("columns = %+v, want a single No Status column", cols)
	}
	if p.StatusOptions() != nil {
		t.Error("StatusOptions() invented options for a project that has no Status field")
	}
}

func TestScheduledTakesOnlyDatedItemsInSpanOrder(t *testing.T) {
	p := testProject()
	got := p.Scheduled()
	if len(got) != 2 {
		t.Fatalf("Scheduled() returned %d items, want the 2 with dates", len(got))
	}
	if got[0].ID != "i3" || got[1].ID != "i2" {
		t.Errorf("Scheduled() order = %s, %s; want the earlier span first", got[0].ID, got[1].ID)
	}
}

func TestFieldLookupsAreCaseInsensitive(t *testing.T) {
	p := testProject()
	f, ok := p.Field("status")
	if !ok || f.ID != "f1" {
		t.Fatalf("Field(\"status\") = %+v, %v", f, ok)
	}
	o, ok := f.OptionByName("IN PROGRESS")
	if !ok || o.ID != "s2" {
		t.Errorf("OptionByName(\"IN PROGRESS\") = %+v, %v", o, ok)
	}
	if _, ok := p.Field("Sprint"); ok {
		t.Error("Field() found a field the project does not have")
	}
	if _, ok := f.OptionByName("Blocked"); ok {
		t.Error("OptionByName() found an option the field does not have")
	}
}

func TestLabelNames(t *testing.T) {
	it := Item{Labels: []Label{{Name: "Develop", Color: "5319e7"}, {Name: "Bug", Color: "d73a4a"}}}
	got := it.LabelNames()
	if len(got) != 2 || got[0] != "Develop" || got[1] != "Bug" {
		t.Errorf("LabelNames() = %q", got)
	}
	if got := (Item{}).LabelNames(); len(got) != 0 {
		t.Errorf("LabelNames() on an unlabelled item = %q, want empty", got)
	}
}
