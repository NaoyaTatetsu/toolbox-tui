package github

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// unmarshalItem parses one item exactly as FetchProject would, so the test
// exercises the JSON tags as well as the conversion.
func unmarshalItem(t *testing.T, raw string) Item {
	t.Helper()
	var ri rawItem
	if err := json.Unmarshal([]byte(raw), &ri); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	return convertItem(ri)
}

func TestConvertItemMapsContentAndFields(t *testing.T) {
	it := unmarshalItem(t, `{
      "id": "PVTI_1",
      "type": "ISSUE",
      "updatedAt": "2026-08-28T01:02:03Z",
      "content": {
        "__typename": "Issue",
        "number": 101,
        "title": "設計レビューの反映",
        "body": "first line",
        "url": "https://github.com/example/notes/issues/101",
        "state": "OPEN",
        "repository": {"nameWithOwner": "example/notes"},
        "milestone": {"title": "v1"},
        "labels": {"nodes": [{"name": "Develop", "color": "5319e7"}]},
        "assignees": {"nodes": [{"login": "octocat"}]}
      },
      "fieldValues": {"nodes": [
        {"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "In Progress", "field": {"name": "Status"}},
        {"__typename": "ProjectV2ItemFieldSingleSelectValue", "name": "High", "field": {"name": "Priority"}},
        {"__typename": "ProjectV2ItemFieldDateValue", "date": "2026-08-26", "field": {"name": "Start Date"}},
        {"__typename": "ProjectV2ItemFieldDateValue", "date": "2026-09-10", "field": {"name": "End Date"}},
        {"__typename": "ProjectV2ItemFieldNumberValue", "number": 3, "field": {"name": "Estimate"}},
        {"__typename": "ProjectV2ItemFieldTextValue", "text": "ops", "field": {"name": "Team"}},
        {"__typename": "ProjectV2ItemFieldTextValue", "text": "ignored", "field": {"name": ""}}
      ]}
    }`)

	if it.ID != "PVTI_1" || it.Number != 101 || it.Repo != "example/notes" {
		t.Errorf("content did not land: %+v", it)
	}
	if it.Milestone != "v1" || it.State != "OPEN" || it.Body != "first line" {
		t.Errorf("content did not land: %+v", it)
	}
	if len(it.Labels) != 1 || it.Labels[0].Color != "5319e7" {
		t.Errorf("labels = %+v", it.Labels)
	}
	if len(it.Assignees) != 1 || it.Assignees[0] != "octocat" {
		t.Errorf("assignees = %q", it.Assignees)
	}
	if it.Status != "In Progress" || it.Priority != "High" {
		t.Errorf("status/priority = %q/%q", it.Status, it.Priority)
	}
	if it.StartDate == nil || !it.StartDate.Equal(*day(2026, 8, 26)) {
		t.Errorf("start date = %v", it.StartDate)
	}
	if it.EndDate == nil || !it.EndDate.Equal(*day(2026, 9, 10)) {
		t.Errorf("end date = %v", it.EndDate)
	}
	// Fields the board does not model explicitly still reach the detail pane.
	if it.Extra["Estimate"] != "3" {
		t.Errorf("Extra[Estimate] = %q, want the number formatted without a decimal point", it.Extra["Estimate"])
	}
	if it.Extra["Team"] != "ops" {
		t.Errorf("Extra[Team] = %q", it.Extra["Team"])
	}
	if len(it.Extra) != 2 {
		t.Errorf("Extra = %v, want the two unmodelled fields and nothing for the nameless one", it.Extra)
	}
	if want := time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC); !it.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", it.UpdatedAt, want)
	}
}

// TestConvertItemTitlesADraft covers the card with no issue behind it: its
// title arrives as a field value rather than as content.
func TestConvertItemTitlesADraft(t *testing.T) {
	it := unmarshalItem(t, `{
      "id": "PVTI_2",
      "type": "DRAFT_ISSUE",
      "fieldValues": {"nodes": [
        {"__typename": "ProjectV2ItemFieldTextValue", "title": "Draft with no issue behind it", "field": {"name": "Title"}}
      ]}
    }`)
	if it.Title != "Draft with no issue behind it" {
		t.Errorf("title = %q", it.Title)
	}
	if it.Number != 0 || it.Repo != "" {
		t.Errorf("a draft should have no number or repo: %+v", it)
	}
}

// TestConvertItemNamesTheNameless keeps a redacted or half-loaded card from
// rendering as an empty row.
func TestConvertItemNamesTheNameless(t *testing.T) {
	it := unmarshalItem(t, `{"id": "PVTI_3", "type": "REDACTED", "fieldValues": {"nodes": []}}`)
	if it.Title != "(untitled)" {
		t.Errorf("title = %q, want a placeholder", it.Title)
	}
	if it.Extra == nil {
		t.Error("Extra is nil; the detail pane writes into it")
	}
	if !it.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt = %v, want the zero time for a missing timestamp", it.UpdatedAt)
	}
}

// TestParseDateIsLocalMidnight matters because every "is it overdue" comparison
// is against the user's own midnight, not UTC's.
func TestParseDateIsLocalMidnight(t *testing.T) {
	got := parseDate("2026-08-26")
	if got == nil {
		t.Fatal("parseDate returned nil for a well-formed date")
	}
	if !got.Equal(*day(2026, 8, 26)) {
		t.Errorf("parseDate = %v, want local midnight", got)
	}
	if got.Location() != time.Local {
		t.Errorf("parseDate location = %v, want local", got.Location())
	}
	for _, bad := range []string{"", "2026-13-01", "26/08/2026"} {
		if parseDate(bad) != nil {
			t.Errorf("parseDate(%q) returned a date", bad)
		}
	}
}

func TestLabelIDsReportsWhatTheRepoDoesNotHave(t *testing.T) {
	repo := Repo{Labels: []RepoLabel{
		{ID: "l1", Name: "Develop"},
		{ID: "l2", Name: "Chore"},
	}}
	ids, missing := repo.LabelIDs([]string{"develop", "  ", "Nonesuch", " Chore "})
	if len(ids) != 2 || ids[0] != "l1" || ids[1] != "l2" {
		t.Errorf("ids = %q, want both labels matched case- and space-insensitively", ids)
	}
	if len(missing) != 1 || missing[0] != "Nonesuch" {
		t.Errorf("missing = %q, want the one label the repo does not have", missing)
	}
}

// TestGQLErrorsSpotsAScopeProblem covers the message that saves a user from an
// opaque FORBIDDEN: the token is fine, its scopes are not.
func TestGQLErrorsSpotsAScopeProblem(t *testing.T) {
	err := gqlErrors([]graphQLError{{Message: "Resource not accessible", Type: "FORBIDDEN"}})
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("gqlErrors returned %T", err)
	}
	if !e.Scopes {
		t.Error("a FORBIDDEN error was not read as a scope problem")
	}
	if !strings.Contains(e.Error(), "gh auth refresh") {
		t.Errorf("the error does not say how to fix it: %q", e.Error())
	}

	plain := gqlErrors([]graphQLError{{Message: "Could not resolve to a ProjectV2", Type: "NOT_FOUND"}}).(*Error)
	if plain.Scopes {
		t.Error("a NOT_FOUND error was blamed on scopes")
	}
	if plain.Error() != "Could not resolve to a ProjectV2" {
		t.Errorf("error = %q", plain.Error())
	}
}

func TestHasScope(t *testing.T) {
	scopes := []string{"repo", "read:project"}
	if HasScope(scopes, "project") {
		t.Error("read:project was accepted as project; writing to the board needs the wider scope")
	}
	if !HasScope(scopes, "repo") {
		t.Error("HasScope missed a scope that is there")
	}
	if HasScope(nil, "project") {
		t.Error("HasScope found a scope in a token that reports none")
	}
}
