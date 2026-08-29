package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/NaoyaTatetsu/toolbox-tui/internal/cache"
	"github.com/NaoyaTatetsu/toolbox-tui/internal/config"
	gh "github.com/NaoyaTatetsu/toolbox-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/toolbox-tui/internal/google-calendar"
)

// TestStartupTiming reports how long the first usable frame takes, cold and warm.
func TestStartupTiming(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("live only")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	token, _ := cfg.ResolveToken()
	client := gh.New(token)
	m := New(cfg, client, googlecalendar.NewClient(time.Minute))
	m.width, m.height = 150, 44

	lap := func(label string, fn func()) time.Duration {
		start := time.Now()
		fn()
		d := time.Since(start)
		fmt.Printf("%-34s %8.1f ms\n", label, float64(d.Microseconds())/1000)
		return d
	}

	fmt.Println("--- cold (no cache) ---")
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	cold := m
	lap("cache read (miss)", func() { _ = cold.loadCache()() })
	lap("project fetch → board usable", func() {
		msg := cold.loadProject()()
		tm, _ := cold.Update(msg)
		cold = tm.(Model)
	})
	lap("calendar fetch → calendar usable", func() {
		msg := cold.loadEvents()()
		tm, cmd := cold.Update(msg)
		cold = tm.(Model)
		if cmd != nil {
			cmd()
		}
	})
	// Persist the project too, the way the running program does.
	saveCache(cold.projectCacheKey(), cold.project)()
	fmt.Printf("  → %d items, %d events\n", len(cold.project.Items), len(cold.events))

	fmt.Println("--- warm (cache on disk) ---")
	warm := New(cfg, client, googlecalendar.NewClient(time.Minute))
	warm.width, warm.height = 150, 44
	lap("cache read → first frame usable", func() {
		msg := warm.loadCache()()
		tm, _ := warm.Update(msg)
		warm = tm.(Model)
		_ = warm.render()
	})
	if warm.project == nil {
		t.Fatal("warm start did not restore the project")
	}
	fmt.Printf("  → %d items, %d events on the first frame\n", len(warm.project.Items), len(warm.events))
}
