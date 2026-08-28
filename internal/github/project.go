package github

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const fieldFragments = `
fragment fieldName on ProjectV2FieldConfiguration {
  ... on ProjectV2Field          { name }
  ... on ProjectV2SingleSelectField { name }
  ... on ProjectV2IterationField { name }
}
fragment fieldDefs on ProjectV2FieldConfiguration {
  ... on ProjectV2Field             { id name dataType }
  ... on ProjectV2IterationField    { id name dataType }
  ... on ProjectV2SingleSelectField { id name dataType options { id name color } }
}
fragment itemFields on ProjectV2Item {
  id
  type
  updatedAt
  content {
    __typename
    ... on DraftIssue { title body }
    ... on Issue {
      number title body url state
      repository { nameWithOwner }
      milestone { title }
      labels(first: 20)    { nodes { name color } }
      assignees(first: 10) { nodes { login } }
    }
    ... on PullRequest {
      number title body url state
      repository { nameWithOwner }
      milestone { title }
      labels(first: 20)    { nodes { name color } }
      assignees(first: 10) { nodes { login } }
    }
  }
  fieldValues(first: 30) {
    nodes {
      __typename
      ... on ProjectV2ItemFieldTextValue         { text   field { ...fieldName } }
      ... on ProjectV2ItemFieldDateValue         { date   field { ...fieldName } }
      ... on ProjectV2ItemFieldNumberValue       { number field { ...fieldName } }
      ... on ProjectV2ItemFieldSingleSelectValue { name   field { ...fieldName } }
      ... on ProjectV2ItemFieldIterationValue    { title  field { ...fieldName } }
    }
  }
}
`

// projectQuery is templated on the owner root field because GitHub exposes
// user-owned and org-owned projects under different query roots.
func projectQuery(ownerType string) string {
	root := "user"
	if ownerType == "organization" {
		root = "organization"
	}
	// 100 is GitHub's page maximum, so most boards load in a single round trip.
	// The field definitions are only needed once; @include keeps them out of the
	// follow-up pages.
	return fieldFragments + fmt.Sprintf(`
query($login: String!, $number: Int!, $cursor: String, $withFields: Boolean!) {
  owner: %s(login: $login) {
    projectV2(number: $number) {
      id title url number
      fields(first: 50) @include(if: $withFields) { nodes { ...fieldDefs } }
      items(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { ...itemFields }
      }
    }
  }
}`, root)
}

type rawFieldName struct {
	Name string `json:"name"`
}

type rawFieldDef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Options  []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"options"`
}

type rawFieldValue struct {
	Typename string       `json:"__typename"`
	Text     *string      `json:"text"`
	Date     *string      `json:"date"`
	Number   *float64     `json:"number"`
	Name     *string      `json:"name"`
	Title    *string      `json:"title"`
	Field    rawFieldName `json:"field"`
}

type rawContent struct {
	Typename   string `json:"__typename"`
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Repository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Labels *struct {
		Nodes []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	Assignees *struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
}

type rawItem struct {
	ID          string      `json:"id"`
	Type        string      `json:"type"`
	UpdatedAt   string      `json:"updatedAt"`
	Content     *rawContent `json:"content"`
	FieldValues struct {
		Nodes []rawFieldValue `json:"nodes"`
	} `json:"fieldValues"`
}

type rawProjectResponse struct {
	Owner *struct {
		ProjectV2 *struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			URL    string `json:"url"`
			Number int    `json:"number"`
			Fields struct {
				Nodes []rawFieldDef `json:"nodes"`
			} `json:"fields"`
			Items struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []rawItem `json:"nodes"`
			} `json:"items"`
		} `json:"projectV2"`
	} `json:"owner"`
}

// FetchProject loads the whole board: field definitions plus every item, paging
// until exhausted.
func (c *Client) FetchProject(ctx context.Context, ownerType, login string, number int) (*Project, error) {
	query := projectQuery(ownerType)
	var proj *Project
	cursor := ""

	for {
		vars := map[string]any{
			"login":      login,
			"number":     number,
			"withFields": proj == nil,
		}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		var resp rawProjectResponse
		if err := c.do(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		if resp.Owner == nil || resp.Owner.ProjectV2 == nil {
			return nil, fmt.Errorf("project %s/%d not found (or the token cannot see it)", login, number)
		}
		p := resp.Owner.ProjectV2

		if proj == nil {
			proj = &Project{ID: p.ID, Title: p.Title, URL: p.URL, Number: p.Number}
			for _, f := range p.Fields.Nodes {
				if f.ID == "" {
					continue // an unmodelled field configuration
				}
				fld := Field{ID: f.ID, Name: f.Name, DataType: f.DataType}
				for _, o := range f.Options {
					fld.Options = append(fld.Options, Option{ID: o.ID, Name: o.Name, Color: o.Color})
				}
				proj.Fields = append(proj.Fields, fld)
			}
		}
		for _, ri := range p.Items.Nodes {
			proj.Items = append(proj.Items, convertItem(ri))
		}
		if !p.Items.PageInfo.HasNextPage {
			break
		}
		cursor = p.Items.PageInfo.EndCursor
	}
	return proj, nil
}

func convertItem(ri rawItem) Item {
	it := Item{ID: ri.ID, Type: ri.Type, Extra: map[string]string{}}
	if t, err := time.Parse(time.RFC3339, ri.UpdatedAt); err == nil {
		it.UpdatedAt = t
	}
	if c := ri.Content; c != nil {
		it.Title, it.Body, it.URL, it.State, it.Number = c.Title, c.Body, c.URL, c.State, c.Number
		if c.Repository != nil {
			it.Repo = c.Repository.NameWithOwner
		}
		if c.Milestone != nil {
			it.Milestone = c.Milestone.Title
		}
		if c.Labels != nil {
			for _, l := range c.Labels.Nodes {
				it.Labels = append(it.Labels, Label{Name: l.Name, Color: l.Color})
			}
		}
		if c.Assignees != nil {
			for _, a := range c.Assignees.Nodes {
				it.Assignees = append(it.Assignees, a.Login)
			}
		}
	}

	for _, fv := range ri.FieldValues.Nodes {
		name := fv.Field.Name
		if name == "" {
			continue
		}
		var value string
		switch {
		case fv.Text != nil:
			value = *fv.Text
		case fv.Name != nil:
			value = *fv.Name
		case fv.Title != nil:
			value = *fv.Title
		case fv.Number != nil:
			value = fmt.Sprintf("%g", *fv.Number)
		case fv.Date != nil:
			value = *fv.Date
		default:
			continue
		}

		switch {
		case strings.EqualFold(name, FieldStatus):
			it.Status = value
		case strings.EqualFold(name, FieldPriority):
			it.Priority = value
		case strings.EqualFold(name, FieldStartDate):
			it.StartDate = parseDate(value)
		case strings.EqualFold(name, FieldEndDate):
			it.EndDate = parseDate(value)
		case strings.EqualFold(name, "Title"):
			if it.Title == "" {
				it.Title = value
			}
		default:
			it.Extra[name] = value
		}
	}
	if it.Title == "" {
		it.Title = "(untitled)"
	}
	return it
}

// parseDate reads GitHub's YYYY-MM-DD project dates as local midnight, so that
// comparisons against "today" behave the way the user's calendar does.
func parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil
	}
	return &t
}
