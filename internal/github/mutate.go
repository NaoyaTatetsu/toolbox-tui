package github

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Repo is the target repository for new tasks, along with its label palette.
type Repo struct {
	ID     string
	Owner  string
	Name   string
	Labels []RepoLabel
}

// RepoLabel is a label available on the repository.
type RepoLabel struct {
	ID    string
	Name  string
	Color string
}

// LabelIDs maps label names to their node ids, skipping unknown names. The
// second return value lists names that did not exist on the repo.
func (r Repo) LabelIDs(names []string) (ids []string, missing []string) {
	for _, want := range names {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		var found bool
		for _, l := range r.Labels {
			if strings.EqualFold(l.Name, want) {
				ids = append(ids, l.ID)
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, want)
		}
	}
	return ids, missing
}

const repoQuery = `
query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    id
    labels(first: 100, orderBy: {field: NAME, direction: ASC}) {
      nodes { id name color }
    }
  }
}`

// FetchRepo loads the repository id and its labels, which the task form needs
// before it can create an issue.
func (c *Client) FetchRepo(ctx context.Context, owner, name string) (*Repo, error) {
	var resp struct {
		Repository *struct {
			ID     string `json:"id"`
			Labels struct {
				Nodes []struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Color string `json:"color"`
				} `json:"nodes"`
			} `json:"labels"`
		} `json:"repository"`
	}
	if err := c.do(ctx, repoQuery, map[string]any{"owner": owner, "name": name}, &resp); err != nil {
		return nil, err
	}
	if resp.Repository == nil {
		return nil, fmt.Errorf("repository %s/%s not found", owner, name)
	}
	r := &Repo{ID: resp.Repository.ID, Owner: owner, Name: name}
	for _, l := range resp.Repository.Labels.Nodes {
		r.Labels = append(r.Labels, RepoLabel{ID: l.ID, Name: l.Name, Color: l.Color})
	}
	return r, nil
}

const createIssueMutation = `
mutation($repositoryId: ID!, $title: String!, $body: String, $labelIds: [ID!]) {
  createIssue(input: {repositoryId: $repositoryId, title: $title, body: $body, labelIds: $labelIds}) {
    issue { id number url }
  }
}`

// CreatedIssue is the freshly filed issue.
type CreatedIssue struct {
	ID     string
	Number int
	URL    string
}

// CreateIssue files a new issue in the repository.
func (c *Client) CreateIssue(ctx context.Context, repoID, title, body string, labelIDs []string) (*CreatedIssue, error) {
	vars := map[string]any{"repositoryId": repoID, "title": title}
	if body != "" {
		vars["body"] = body
	}
	if len(labelIDs) > 0 {
		vars["labelIds"] = labelIDs
	}
	var resp struct {
		CreateIssue struct {
			Issue struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				URL    string `json:"url"`
			} `json:"issue"`
		} `json:"createIssue"`
	}
	if err := c.do(ctx, createIssueMutation, vars, &resp); err != nil {
		return nil, err
	}
	i := resp.CreateIssue.Issue
	return &CreatedIssue{ID: i.ID, Number: i.Number, URL: i.URL}, nil
}

const addItemMutation = `
mutation($projectId: ID!, $contentId: ID!) {
  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
    item { id }
  }
}`

// AddItem puts an existing issue/PR onto the project board and returns the new
// project item id. Adding an item that is already on the board is idempotent —
// GitHub returns the existing item.
func (c *Client) AddItem(ctx context.Context, projectID, contentID string) (string, error) {
	var resp struct {
		AddProjectV2ItemByID struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}
	vars := map[string]any{"projectId": projectID, "contentId": contentID}
	if err := c.do(ctx, addItemMutation, vars, &resp); err != nil {
		return "", err
	}
	return resp.AddProjectV2ItemByID.Item.ID, nil
}

const setFieldMutation = `
mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $value: ProjectV2FieldValue!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $projectId, itemId: $itemId, fieldId: $fieldId, value: $value
  }) {
    projectV2Item { id }
  }
}`

// SetSingleSelect sets a single-select field (Status, Priority) by option id.
func (c *Client) SetSingleSelect(ctx context.Context, projectID, itemID, fieldID, optionID string) error {
	return c.setField(ctx, projectID, itemID, fieldID, map[string]any{"singleSelectOptionId": optionID})
}

// SetDate sets a date field. GitHub stores project dates as bare calendar days.
func (c *Client) SetDate(ctx context.Context, projectID, itemID, fieldID string, d time.Time) error {
	return c.setField(ctx, projectID, itemID, fieldID, map[string]any{"date": d.Format("2006-01-02")})
}

// SetText sets a text field.
func (c *Client) SetText(ctx context.Context, projectID, itemID, fieldID, text string) error {
	return c.setField(ctx, projectID, itemID, fieldID, map[string]any{"text": text})
}

func (c *Client) setField(ctx context.Context, projectID, itemID, fieldID string, value map[string]any) error {
	vars := map[string]any{
		"projectId": projectID,
		"itemId":    itemID,
		"fieldId":   fieldID,
		"value":     value,
	}
	return c.do(ctx, setFieldMutation, vars, nil)
}

// NewTask is the payload the registration form produces.
type NewTask struct {
	Title    string
	Body     string
	Labels   []string
	Status   string
	Priority string
	Start    *time.Time
	End      *time.Time
}

// CreateTask files an issue, adds it to the project, and applies the project
// fields. Field failures are collected rather than aborting, because a task
// that exists with partial metadata beats one that silently vanished.
func (c *Client) CreateTask(ctx context.Context, p *Project, repo *Repo, t NewTask) (*CreatedIssue, error) {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	labelIDs, missing := repo.LabelIDs(t.Labels)
	issue, err := c.CreateIssue(ctx, repo.ID, title, t.Body, labelIDs)
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	itemID, err := c.AddItem(ctx, p.ID, issue.ID)
	if err != nil {
		return issue, fmt.Errorf("issue #%d created, but adding it to the project failed: %w", issue.Number, err)
	}

	var problems []string
	setSelect := func(fieldName, value string) {
		if value == "" {
			return
		}
		f, ok := p.Field(fieldName)
		if !ok {
			problems = append(problems, fmt.Sprintf("no %s field on this project", fieldName))
			return
		}
		opt, ok := f.OptionByName(value)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s has no option %q", fieldName, value))
			return
		}
		if err := c.SetSingleSelect(ctx, p.ID, itemID, f.ID, opt.ID); err != nil {
			problems = append(problems, fmt.Sprintf("set %s: %v", fieldName, err))
		}
	}
	setDate := func(fieldName string, d *time.Time) {
		if d == nil {
			return
		}
		f, ok := p.Field(fieldName)
		if !ok {
			problems = append(problems, fmt.Sprintf("no %s field on this project", fieldName))
			return
		}
		if err := c.SetDate(ctx, p.ID, itemID, f.ID, *d); err != nil {
			problems = append(problems, fmt.Sprintf("set %s: %v", fieldName, err))
		}
	}

	setSelect(FieldStatus, t.Status)
	setSelect(FieldPriority, t.Priority)
	setDate(FieldStartDate, t.Start)
	setDate(FieldEndDate, t.End)

	if len(missing) > 0 {
		problems = append(problems, "labels not on repo, skipped: "+strings.Join(missing, ", "))
	}
	if len(problems) > 0 {
		return issue, fmt.Errorf("issue #%d created, with problems: %s", issue.Number, strings.Join(problems, "; "))
	}
	return issue, nil
}

// MoveStatus sets an item's Status to the named option.
func (c *Client) MoveStatus(ctx context.Context, p *Project, itemID, status string) error {
	f, ok := p.Field(FieldStatus)
	if !ok {
		return fmt.Errorf("this project has no %s field", FieldStatus)
	}
	opt, ok := f.OptionByName(status)
	if !ok {
		return fmt.Errorf("%s has no option %q", FieldStatus, status)
	}
	return c.SetSingleSelect(ctx, p.ID, itemID, f.ID, opt.ID)
}
