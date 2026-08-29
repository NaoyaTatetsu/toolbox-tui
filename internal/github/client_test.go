package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// call is one GraphQL round trip the client made.
type call struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// stubTransport answers every request from a function instead of the network,
// and records what was asked. The endpoint is a constant, so the seam is the
// client's own http.Client rather than a base URL.
type stubTransport struct {
	t       *testing.T
	calls   *[]call
	respond func(c call) string
}

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	var c call
	if err := json.Unmarshal(body, &c); err != nil {
		s.t.Fatalf("the client sent a body that is not GraphQL JSON: %s", body)
	}
	*s.calls = append(*s.calls, c)

	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json")
	w.WriteString(s.respond(c))
	return w.Result(), nil
}

// stubClient returns a client whose requests are answered by respond, plus the
// slice recording them.
func stubClient(t *testing.T, respond func(c call) string) (*Client, *[]call) {
	t.Helper()
	calls := &[]call{}
	c := New("test-token")
	c.http = &http.Client{Transport: stubTransport{t: t, calls: calls, respond: respond}}
	return c, calls
}

// TestFetchProjectPagesUntilExhausted covers the loop that made the board's
// 100-item page limit invisible: the second page must be asked for with the
// cursor, and without re-fetching the field definitions.
func TestFetchProjectPagesUntilExhausted(t *testing.T) {
	page := func(id string, hasNext bool, cursor string) string {
		return `{"data":{"owner":{"projectV2":{
          "id":"P_1","title":"Tasks","url":"https://github.com/users/example/projects/4","number":4,
          "fields":{"nodes":[{"id":"f1","name":"Status","dataType":"SINGLE_SELECT",
            "options":[{"id":"s1","name":"Todo","color":"GREEN"}]}]},
          "items":{"pageInfo":{"hasNextPage":` + boolJSON(hasNext) + `,"endCursor":"` + cursor + `"},
            "nodes":[{"id":"` + id + `","type":"ISSUE","content":{"title":"` + id + `"},"fieldValues":{"nodes":[]}}]}
        }}}}`
	}
	client, calls := stubClient(t, func(c call) string {
		if c.Variables["cursor"] == nil {
			return page("first", true, "CURSOR-2")
		}
		return page("second", false, "")
	})

	p, err := client.FetchProject(context.Background(), "user", "example", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 {
		t.Fatalf("the client made %d calls, want one per page", len(*calls))
	}
	if got := (*calls)[1].Variables["cursor"]; got != "CURSOR-2" {
		t.Errorf("the second page was asked for with cursor %v, want CURSOR-2", got)
	}
	// The field definitions do not change between pages, so only the first
	// request pays for them.
	if (*calls)[0].Variables["withFields"] != true || (*calls)[1].Variables["withFields"] != false {
		t.Errorf("withFields = %v then %v, want true then false",
			(*calls)[0].Variables["withFields"], (*calls)[1].Variables["withFields"])
	}
	if len(p.Items) != 2 || p.Items[0].ID != "first" || p.Items[1].ID != "second" {
		t.Errorf("items = %+v, want both pages in order", p.Items)
	}
	if len(p.Fields) != 1 || len(p.Fields[0].Options) != 1 {
		t.Errorf("fields = %+v, want the one Status field with its option", p.Fields)
	}
}

func TestFetchProjectExplainsAnEmptyOwner(t *testing.T) {
	client, _ := stubClient(t, func(call) string { return `{"data":{"owner":null}}` })
	_, err := client.FetchProject(context.Background(), "organization", "example", 4)
	if err == nil {
		t.Fatal("FetchProject succeeded with no project in the response")
	}
	if !strings.Contains(err.Error(), "example/4") {
		t.Errorf("error = %q, want it to name the project", err)
	}
}

// TestGraphQLErrorsReachTheCaller keeps a scope failure legible: GitHub answers
// 200 with an errors array, so the status code alone says nothing.
func TestGraphQLErrorsReachTheCaller(t *testing.T) {
	client, _ := stubClient(t, func(call) string {
		return `{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`
	})
	_, err := client.FetchProject(context.Background(), "user", "example", 4)
	if err == nil {
		t.Fatal("FetchProject ignored the GraphQL errors")
	}
	if !strings.Contains(err.Error(), "gh auth refresh") {
		t.Errorf("error = %q, want the scope advice", err)
	}
}

// TestCreateTaskAppliesEveryFieldItWasGiven pins the order of the flow: the
// issue exists first, then it joins the board, then its fields are set.
func TestCreateTaskAppliesEveryFieldItWasGiven(t *testing.T) {
	client, calls := stubClient(t, func(c call) string {
		switch {
		case strings.Contains(c.Query, "createIssue"):
			return `{"data":{"createIssue":{"issue":{"id":"I_1","number":42,"url":"https://example.invalid/42"}}}}`
		case strings.Contains(c.Query, "addProjectV2ItemById"):
			return `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_1"}}}}`
		default:
			return `{"data":{}}`
		}
	})

	p := testProject()
	p.ID = "P_1"
	repo := &Repo{ID: "R_1", Labels: []RepoLabel{{ID: "l1", Name: "Develop"}}}
	start, end := time.Date(2026, 8, 26, 0, 0, 0, 0, time.Local), time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)

	issue, err := client.CreateTask(context.Background(), p, repo, NewTask{
		Title: "  Write the report  ", Body: "why",
		Labels: []string{"Develop"}, Status: "Todo", Priority: "High",
		Start: &start, End: &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 42 {
		t.Errorf("issue = %+v", issue)
	}
	// create + add + status + priority + start + end
	if len(*calls) != 6 {
		t.Fatalf("the client made %d calls, want 6", len(*calls))
	}
	first := (*calls)[0]
	if title := first.Variables["title"]; title != "Write the report" {
		t.Errorf("title = %q, want it trimmed", title)
	}
	if !strings.Contains(first.Query, "createIssue") {
		t.Errorf("the first call was not the issue: %q", first.Query)
	}
	if got := (*calls)[2].Variables["value"]; got == nil {
		t.Error("the Status mutation carried no value")
	}
	// A date field is written as GitHub's own YYYY-MM-DD, whatever the clock.
	body, _ := json.Marshal((*calls)[4].Variables["value"])
	if !strings.Contains(string(body), "2026-08-26") {
		t.Errorf("start date sent as %s, want 2026-08-26", body)
	}
}

// TestCreateTaskKeepsTheIssueWhenTheFieldsFail is the promise in its comment: a
// task that exists with partial metadata beats one that silently vanished.
func TestCreateTaskKeepsTheIssueWhenTheFieldsFail(t *testing.T) {
	client, _ := stubClient(t, func(c call) string {
		switch {
		case strings.Contains(c.Query, "createIssue"):
			return `{"data":{"createIssue":{"issue":{"id":"I_1","number":42,"url":"https://example.invalid/42"}}}}`
		case strings.Contains(c.Query, "addProjectV2ItemById"):
			return `{"data":{"addProjectV2ItemById":{"item":{"id":"PVTI_1"}}}}`
		default:
			return `{"errors":[{"type":"NOT_FOUND","message":"field is gone"}]}`
		}
	})

	p := testProject()
	repo := &Repo{ID: "R_1"}
	issue, err := client.CreateTask(context.Background(), p, repo, NewTask{
		Title:  "Write the report",
		Status: "Todo",
		Labels: []string{"Nonesuch"},
	})
	if issue == nil || issue.Number != 42 {
		t.Fatalf("the created issue was thrown away: %+v", issue)
	}
	if err == nil {
		t.Fatal("the field failure was not reported")
	}
	for _, want := range []string{"#42 created", "set Status", "labels not on repo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestCreateTaskNeedsATitle(t *testing.T) {
	client, calls := stubClient(t, func(call) string { return `{"data":{}}` })
	if _, err := client.CreateTask(context.Background(), testProject(), &Repo{ID: "R_1"}, NewTask{Title: "   "}); err == nil {
		t.Error("CreateTask filed an issue with no title")
	}
	if len(*calls) != 0 {
		t.Errorf("the client called GitHub %d times before validating the title", len(*calls))
	}
}

func TestMoveStatusRefusesAnOptionTheBoardDoesNotHave(t *testing.T) {
	client, calls := stubClient(t, func(call) string { return `{"data":{}}` })
	p := testProject()

	if err := client.MoveStatus(context.Background(), p, "PVTI_1", "Blocked"); err == nil {
		t.Error("MoveStatus accepted a status the Status field does not offer")
	}
	if err := client.MoveStatus(context.Background(), &Project{}, "PVTI_1", "Todo"); err == nil {
		t.Error("MoveStatus accepted a project with no Status field")
	}
	if len(*calls) != 0 {
		t.Errorf("the client called GitHub %d times for a move it could not make", len(*calls))
	}

	if err := client.MoveStatus(context.Background(), p, "PVTI_1", "in progress"); err != nil {
		t.Errorf("MoveStatus rejected a known status in the wrong case: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("the client made %d calls for one move", len(*calls))
	}
	if got := (*calls)[0].Variables["itemId"]; got != "PVTI_1" {
		t.Errorf("the mutation moved %v, want PVTI_1", got)
	}
	if got := (*calls)[0].Variables["fieldId"]; got != "f1" {
		t.Errorf("the mutation wrote field %v, want the Status field", got)
	}
}

func boolJSON(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
