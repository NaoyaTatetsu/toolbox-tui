package github

import (
	"sort"
	"strings"
	"time"
)

// Field data types we care about. Everything else is ignored.
const (
	FieldSingleSelect = "SINGLE_SELECT"
	FieldDate         = "DATE"
	FieldText         = "TEXT"
	FieldNumber       = "NUMBER"
)

// Well-known field names on the board. These are conventions, not GitHub
// guarantees, so lookups fall back gracefully when a project renames them.
const (
	FieldStatus    = "Status"
	FieldPriority  = "Priority"
	FieldStartDate = "Start Date"
	FieldEndDate   = "End Date"
)

// Field is one column definition of the project.
type Field struct {
	ID       string
	Name     string
	DataType string
	Options  []Option // single-select only
}

// Option is a single-select choice, e.g. Status="In Progress".
type Option struct {
	ID    string
	Name  string
	Color string // GitHub's palette name: RED, YELLOW, …
}

// OptionByName finds a select option case-insensitively.
func (f Field) OptionByName(name string) (Option, bool) {
	for _, o := range f.Options {
		if strings.EqualFold(o.Name, name) {
			return o, true
		}
	}
	return Option{}, false
}

// Project is the board metadata plus its items.
type Project struct {
	ID     string
	Title  string
	URL    string
	Number int
	Fields []Field
	Items  []Item
}

// Field looks up a field definition by name, case-insensitively.
func (p *Project) Field(name string) (Field, bool) {
	for _, f := range p.Fields {
		if strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return Field{}, false
}

// StatusOptions returns the Status field's options in project order, or nil if
// the project has no Status field.
func (p *Project) StatusOptions() []Option {
	f, ok := p.Field(FieldStatus)
	if !ok {
		return nil
	}
	return f.Options
}

// Item is one card on the board.
type Item struct {
	ID        string // ProjectV2Item node id — the handle for field mutations
	Type      string // ISSUE, PULL_REQUEST, DRAFT_ISSUE, REDACTED
	Title     string
	Body      string
	URL       string
	Number    int    // issue/PR number; 0 for drafts
	Repo      string // "owner/name"; empty for drafts
	State     string // OPEN / CLOSED / MERGED
	Labels    []Label
	Assignees []string
	Status    string
	Priority  string
	StartDate *time.Time
	EndDate   *time.Time
	Milestone string
	UpdatedAt time.Time
	// Extra holds single-select/text/date values for fields we do not model
	// explicitly, keyed by field name.
	Extra map[string]string
}

// Label is an issue label with its hex colour (no leading '#').
type Label struct {
	Name  string
	Color string
}

// LabelNames returns just the label names, for compact rendering.
func (i Item) LabelNames() []string {
	out := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		out = append(out, l.Name)
	}
	return out
}

// Overdue reports whether the item has a past end date and is not Done/closed.
func (i Item) Overdue(now time.Time) bool {
	if i.EndDate == nil || i.IsDone() {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return i.EndDate.Before(today)
}

// IsDone reports whether the item is finished, by status or by issue state.
func (i Item) IsDone() bool {
	return strings.EqualFold(i.Status, "Done") || i.State == "CLOSED" || i.State == "MERGED"
}

// PriorityRank orders High < Middle < Low < unset for sorting (lower is more
// urgent). Unknown values sort last but before unset.
func (i Item) PriorityRank() int {
	switch strings.ToLower(i.Priority) {
	case "high", "urgent", "p0":
		return 0
	case "middle", "medium", "p1":
		return 1
	case "low", "p2":
		return 2
	case "":
		return 4
	default:
		return 3
	}
}

// SortItems orders cards the way a person triages: overdue first, then by end
// date, then priority, then title. Items without dates sink below dated ones.
func SortItems(items []Item, now time.Time) {
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		if xo, yo := x.Overdue(now), y.Overdue(now); xo != yo {
			return xo
		}
		if (x.EndDate == nil) != (y.EndDate == nil) {
			return x.EndDate != nil
		}
		if x.EndDate != nil && !x.EndDate.Equal(*y.EndDate) {
			return x.EndDate.Before(*y.EndDate)
		}
		if xr, yr := x.PriorityRank(), y.PriorityRank(); xr != yr {
			return xr < yr
		}
		return x.Title < y.Title
	})
}

// Column is one board lane.
type Column struct {
	Name  string
	Color string
	Items []Item
}

// Board groups items into columns following the project's Status options,
// reordered by prefer and optionally dropping Done. Items whose status is unset
// land in a trailing "No Status" column.
func (p *Project) Board(prefer []string, hideDone bool, now time.Time) []Column {
	opts := p.StatusOptions()
	names := make([]string, 0, len(opts)+1)
	colors := map[string]string{}
	seen := map[string]bool{}

	add := func(name, color string) {
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		names = append(names, name)
		colors[key] = color
	}
	// Preferred order first, but only for statuses the project actually has.
	for _, want := range prefer {
		for _, o := range opts {
			if strings.EqualFold(o.Name, want) {
				add(o.Name, o.Color)
			}
		}
	}
	for _, o := range opts {
		add(o.Name, o.Color)
	}

	buckets := map[string][]Item{}
	var unset []Item
	for _, it := range p.Items {
		if it.Status == "" {
			unset = append(unset, it)
			continue
		}
		key := strings.ToLower(it.Status)
		if !seen[key] {
			// Status value the field no longer offers; keep it visible.
			add(it.Status, "GRAY")
		}
		buckets[key] = append(buckets[key], it)
	}

	cols := make([]Column, 0, len(names)+1)
	for _, n := range names {
		key := strings.ToLower(n)
		if hideDone && strings.EqualFold(n, "Done") {
			continue
		}
		items := buckets[key]
		SortItems(items, now)
		cols = append(cols, Column{Name: n, Color: colors[key], Items: items})
	}
	if len(unset) > 0 {
		SortItems(unset, now)
		cols = append(cols, Column{Name: "No Status", Color: "GRAY", Items: unset})
	}
	return cols
}

// Scheduled returns items that can be placed on a timeline, i.e. those with at
// least one of start/end date, sorted by their effective start.
func (p *Project) Scheduled() []Item {
	var out []Item
	for _, it := range p.Items {
		if it.StartDate != nil || it.EndDate != nil {
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		sa, ea := out[a].Span()
		sb, eb := out[b].Span()
		if !sa.Equal(sb) {
			return sa.Before(sb)
		}
		if !ea.Equal(eb) {
			return ea.Before(eb)
		}
		return out[a].Title < out[b].Title
	})
	return out
}

// Span returns the item's inclusive [start, end] on the timeline. An item with
// only one of the two dates occupies that single day.
func (i Item) Span() (time.Time, time.Time) {
	switch {
	case i.StartDate != nil && i.EndDate != nil:
		if i.EndDate.Before(*i.StartDate) {
			return *i.EndDate, *i.StartDate
		}
		return *i.StartDate, *i.EndDate
	case i.StartDate != nil:
		return *i.StartDate, *i.StartDate
	case i.EndDate != nil:
		return *i.EndDate, *i.EndDate
	}
	return time.Time{}, time.Time{}
}
