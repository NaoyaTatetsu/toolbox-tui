package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NaoyaTatetsu/task-tui/internal/config"
	gh "github.com/NaoyaTatetsu/task-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/task-tui/internal/google-calendar"
)

// TestLiveRender renders every view against the real configured project. It is
// opt-in because it needs network and credentials:
//
//	LIVE=1 go test ./internal/ui -run Live -v
func TestLiveRender(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 to render against the configured project")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("no usable config: %v", err)
	}
	token, err := cfg.ResolveToken()
	if err != nil {
		t.Skipf("no token: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := gh.New(token)
	project, err := client.FetchProject(ctx, cfg.GitHub.OwnerType, cfg.GitHub.Owner, cfg.GitHub.ProjectNumber)
	if err != nil {
		t.Fatalf("fetch project: %v", err)
	}

	m := New(cfg, client, googlecalendar.NewClient(time.Minute))
	m.width, m.height = 150, 44
	m.project = project
	if owner, name, ok := cfg.RepoParts(); ok {
		if repo, err := client.FetchRepo(ctx, owner, name); err == nil {
			m.repo = repo
		}
	}
	if len(cfg.Calendar.Sources) > 0 {
		var srcs []googlecalendar.Source
		for _, s := range cfg.Calendar.Sources {
			srcs = append(srcs, googlecalendar.Source{Name: s.Name, URL: s.URL, Color: s.Color})
		}
		evs, errs := m.calendar.Events(ctx, srcs, m.now.AddDate(0, -1, 0), m.now.AddDate(0, 6, 0), time.Local)
		for _, e := range errs {
			t.Logf("calendar: %v", e)
		}
		m.events = evs
	}
	m.rebuild()

	for _, view := range []viewID{viewBoard, viewRoadmap, viewCalendar} {
		m.view = view
		out := m.View()
		for i, line := range strings.Split(out, "\n") {
			if w := lipglossWidth(line); w > m.width {
				t.Errorf("%v line %d is %d cells wide, want <= %d", view, i, w, m.width)
			}
		}
		fmt.Printf("\n===== %v =====\n%s\n", view, out)
	}
}
