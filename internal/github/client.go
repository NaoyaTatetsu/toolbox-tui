package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const endpoint = "https://api.github.com/graphql"

// Client is a minimal GitHub GraphQL client. It exists instead of a generated
// client because the project schema is small and the queries are hand-tuned for
// the two round-trip budget the TUI wants.
type Client struct {
	token string
	http  *http.Client
}

// New returns a client authenticating with the given token.
func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// do executes a GraphQL operation and unmarshals `data` into out.
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "task-tui")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var gr graphQLResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&gr); err != nil {
		return fmt.Errorf("github %s: decode response: %w", resp.Status, err)
	}
	if len(gr.Errors) > 0 {
		return fmt.Errorf("github: %w", gqlErrors(gr.Errors))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(gr.Data, out)
}

// Error carries GraphQL errors so callers can detect missing scopes.
type Error struct {
	Messages []string
	Scopes   bool // true when the failure looks like an insufficient-scope error
}

func (e *Error) Error() string {
	msg := strings.Join(e.Messages, "; ")
	if e.Scopes {
		msg += "\n\nThis token lacks the `project` scope. Run:\n    gh auth refresh -s project,read:project,repo"
	}
	return msg
}

func gqlErrors(errs []graphQLError) error {
	e := &Error{}
	for _, ge := range errs {
		e.Messages = append(e.Messages, ge.Message)
		low := strings.ToLower(ge.Message)
		if strings.Contains(low, "scope") || ge.Type == "FORBIDDEN" || ge.Type == "INSUFFICIENT_SCOPES" {
			e.Scopes = true
		}
	}
	return e
}

// Scopes reports the OAuth scopes attached to the token, as GitHub sees them.
// Writing to a project needs `project`; `read:project` alone is not enough, and
// the resulting GraphQL error is opaque, so the TUI checks up front.
func (c *Client) Scopes(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "task-tui")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw := resp.Header.Get("X-OAuth-Scopes")
	if raw == "" {
		// Fine-grained PATs and GitHub Apps do not report classic scopes.
		return nil, nil
	}
	var scopes []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			scopes = append(scopes, s)
		}
	}
	return scopes, nil
}

// HasScope reports whether the list contains the named scope.
func HasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// Login returns the authenticated user's handle.
func (c *Client) Login(ctx context.Context) (string, error) {
	var resp struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.do(ctx, `query { viewer { login } }`, nil, &resp); err != nil {
		return "", err
	}
	return resp.Viewer.Login, nil
}
