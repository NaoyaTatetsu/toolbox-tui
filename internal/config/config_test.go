package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig points the loader at a temporary file holding body.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOOLBOX_TUI_CONFIG", path)
	return path
}

func TestLoadAppliesDefaults(t *testing.T) {
	writeConfig(t, `
[github]
owner          = "example-user"
project_number = 4
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub.OwnerType != "user" {
		t.Errorf("owner_type = %q, want the user default", c.GitHub.OwnerType)
	}
	if c.UI.RoadmapDays != 42 {
		t.Errorf("roadmap_days = %d, want 42", c.UI.RoadmapDays)
	}
	if len(c.UI.StatusOrder) == 0 || c.UI.StatusOrder[0] != "Pending" {
		t.Errorf("status_order = %q, want the built-in order", c.UI.StatusOrder)
	}
	// Absent means on, so a config that says nothing about the mouse gets one.
	if !c.UI.MouseEnabled() {
		t.Error("the mouse is off in a config that never mentions it")
	}
}

func TestLoadKeepsWhatTheFileSays(t *testing.T) {
	writeConfig(t, `
[github]
owner          = "example-org"
owner_type     = "organization"
project_number = 7
default_repo   = "example/notes"

[ui]
status_order       = ["Todo", "Done"]
hide_done          = true
roadmap_days       = 14
mouse              = false
scroll_ticks       = 1
scroll_interval_ms = 0

[[calendar.sources]]
name  = "personal"
url   = "https://calendar.example.invalid/basic.ics"
email = "me@example.com"
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.GitHub.OwnerType != "organization" || c.GitHub.ProjectNumber != 7 {
		t.Errorf("github = %+v", c.GitHub)
	}
	if len(c.UI.StatusOrder) != 2 || !c.UI.HideDone || c.UI.RoadmapDays != 14 {
		t.Errorf("ui = %+v", c.UI)
	}
	if c.UI.MouseEnabled() {
		t.Error("mouse = false was ignored")
	}
	// An explicit zero interval removes the cap; it is not "unset, use 60ms".
	if got := c.UI.ScrollInterval(); got != 0 {
		t.Errorf("scroll interval = %v, want no cap", got)
	}
	if len(c.Calendar.Sources) != 1 || c.Calendar.Sources[0].Email != "me@example.com" {
		t.Errorf("calendar sources = %+v", c.Calendar.Sources)
	}
}

func TestLoadRejectsConfigItCannotAct(t *testing.T) {
	cases := map[string]string{
		"no owner": `
[github]
project_number = 4
`,
		"no project number": `
[github]
owner = "example-user"
`,
		"owner_type typo": `
[github]
owner          = "example-user"
owner_type     = "org"
project_number = 4
`,
		"repo without an owner": `
[github]
owner          = "example-user"
project_number = 4
default_repo   = "notes"
`,
	}
	for name, body := range cases {
		writeConfig(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: Load() accepted the config", name)
		}
	}
}

// TestLoadSaysHowToStart is the first thing a new user sees, so it has to name
// the file and the command that writes one.
func TestLoadSaysHowToStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.toml")
	t.Setenv("TOOLBOX_TUI_CONFIG", path)
	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with no config file")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "tt init") {
		t.Errorf("error = %q, want it to name the path and `tt init`", err)
	}
}

func TestLoadReportsBrokenToml(t *testing.T) {
	writeConfig(t, "[github\nowner = \"x\"")
	if _, err := Load(); err == nil {
		t.Error("Load() accepted a file that is not TOML")
	}
}

func TestPathPrefersTheExplicitOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	t.Setenv("TOOLBOX_TUI_CONFIG", "/explicit/config.toml")
	if got := Path(); got != "/explicit/config.toml" {
		t.Errorf("Path() = %q, want the override", got)
	}

	t.Setenv("TOOLBOX_TUI_CONFIG", "")
	if got, want := Path(), filepath.Join("/xdg", "toolbox-tui", "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestRepoParts(t *testing.T) {
	cases := map[string]struct {
		owner, name string
		ok          bool
	}{
		"example/notes": {"example", "notes", true},
		"notes":         {"", "", false},
		"/notes":        {"", "", false},
		"example/":      {"", "", false},
		"":              {"", "", false},
	}
	for repo, want := range cases {
		c := &Config{}
		c.GitHub.DefaultRepo = repo
		owner, name, ok := c.RepoParts()
		if owner != want.owner || name != want.name || ok != want.ok {
			t.Errorf("RepoParts(%q) = %q, %q, %v; want %q, %q, %v",
				repo, owner, name, ok, want.owner, want.name, want.ok)
		}
	}
}

// TestResolveTokenPrefersTheConfig covers the discovery chain without reaching
// the `gh` fallback, which depends on the machine the test runs on.
func TestResolveTokenPrefersTheConfig(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-env")
	t.Setenv("GH_TOKEN", "")

	c := &Config{}
	c.GitHub.Token = "from-config"
	if got, err := c.ResolveToken(); err != nil || got != "from-config" {
		t.Errorf("ResolveToken() = %q, %v; want the config value", got, err)
	}

	c.GitHub.Token = ""
	if got, err := c.ResolveToken(); err != nil || got != "from-env" {
		t.Errorf("ResolveToken() = %q, %v; want GITHUB_TOKEN", got, err)
	}

	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "from-gh-env")
	if got, err := c.ResolveToken(); err != nil || got != "from-gh-env" {
		t.Errorf("ResolveToken() = %q, %v; want GH_TOKEN", got, err)
	}
}

func TestTicksPerStepGuardsAgainstNonsense(t *testing.T) {
	for _, ticks := range []int{0, -3} {
		if got := (UI{ScrollTicks: ticks}).TicksPerStep(); got != DefaultScrollTicks {
			t.Errorf("TicksPerStep(%d) = %d, want the default", ticks, got)
		}
	}
	if got := (UI{ScrollTicks: 5}).TicksPerStep(); got != 5 {
		t.Errorf("TicksPerStep(5) = %d", got)
	}
}

func TestScrollIntervalDistinguishesUnsetFromZero(t *testing.T) {
	if got := (UI{}).ScrollInterval(); got != DefaultScrollIntervalMS*time.Millisecond {
		t.Errorf("unset interval = %v, want the default", got)
	}
	zero, negative := 0, -50
	if got := (UI{ScrollIntervalMS: &zero}).ScrollInterval(); got != 0 {
		t.Errorf("explicit 0 = %v, want no cap", got)
	}
	if got := (UI{ScrollIntervalMS: &negative}).ScrollInterval(); got != 0 {
		t.Errorf("negative interval = %v, want no cap rather than a negative one", got)
	}
}

func TestScrollPresetsAreOrderedAndReachable(t *testing.T) {
	if DefaultScrollLevel < 0 || DefaultScrollLevel >= len(ScrollPresets) {
		t.Fatalf("DefaultScrollLevel = %d, outside the ladder", DefaultScrollLevel)
	}
	// The defaults the config writes must be the rung the TUI starts on, or the
	// sensitivity readout opens on the wrong step.
	if p := ScrollPresets[DefaultScrollLevel]; p.Ticks != DefaultScrollTicks || p.IntervalMS != DefaultScrollIntervalMS {
		t.Errorf("ScrollPresets[%d] = %+v, want the documented defaults", DefaultScrollLevel, p)
	}
	// Slowest to fastest: neither number may rise as the level does.
	for i := 1; i < len(ScrollPresets); i++ {
		if ScrollPresets[i].Ticks > ScrollPresets[i-1].Ticks {
			t.Errorf("rung %d costs more ticks than rung %d", i, i-1)
		}
		if ScrollPresets[i].IntervalMS > ScrollPresets[i-1].IntervalMS {
			t.Errorf("rung %d waits longer than rung %d", i, i-1)
		}
	}
	if got := (ScrollPreset{IntervalMS: 0}).StepsPerSecond(); got != 0 {
		t.Errorf("StepsPerSecond() with no cap = %d, want 0", got)
	}
	if got := (ScrollPreset{IntervalMS: 100}).StepsPerSecond(); got != 10 {
		t.Errorf("StepsPerSecond() = %d, want 10", got)
	}
}

func TestNearestScrollLevelLandsOnTheLadder(t *testing.T) {
	for i, p := range ScrollPresets {
		if got := NearestScrollLevel(p.Ticks, p.IntervalMS); got != i {
			t.Errorf("NearestScrollLevel(%d, %d) = %d, want %d", p.Ticks, p.IntervalMS, got, i)
		}
	}
	// A hand-edited pair that is on no rung still has to land somewhere.
	if got := NearestScrollLevel(3, 90); got != 2 {
		t.Errorf("NearestScrollLevel(3, 90) = %d, want the {3, 100} rung", got)
	}
}
