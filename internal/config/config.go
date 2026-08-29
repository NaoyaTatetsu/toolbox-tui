package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration, loaded from ~/.config/toolbox-tui/config.toml.
type Config struct {
	GitHub   GitHub   `toml:"github"`
	Calendar Calendar `toml:"calendar"`
	UI       UI       `toml:"ui"`
}

type GitHub struct {
	// Owner is the user (or org) that owns the project board.
	Owner string `toml:"owner"`
	// OwnerType is "user" or "organization".
	OwnerType string `toml:"owner_type"`
	// ProjectNumber is the number in the project URL (…/projects/4).
	ProjectNumber int `toml:"project_number"`
	// DefaultRepo is where newly registered tasks are filed, as "owner/name".
	// Labels only exist on real issues, so task registration needs a repo.
	DefaultRepo string `toml:"default_repo"`
	// Token overrides the token discovery chain (GITHUB_TOKEN, then `gh auth token`).
	Token string `toml:"token"`
}

type Calendar struct {
	// Sources are Google Calendar "secret address in iCal format" URLs.
	Sources []CalendarSource `toml:"sources"`
}

type CalendarSource struct {
	Name  string `toml:"name"`
	URL   string `toml:"url"`
	Color string `toml:"color"` // optional hex, e.g. "#7aa2f7"
}

type UI struct {
	// StatusOrder fixes the left-to-right column order on the board. Statuses
	// not listed here are appended in the order the project reports them.
	StatusOrder []string `toml:"status_order"`
	// HideDone drops the Done column from the board.
	HideDone bool `toml:"hide_done"`
	// RoadmapDays is the default window width of the roadmap in days.
	RoadmapDays int `toml:"roadmap_days"`
	// Mouse enables trackpad and wheel scrolling. It is on unless set to false.
	// Turning it off restores the terminal's own click-and-drag text selection.
	Mouse *bool `toml:"mouse"`
	// ScrollTicks is how many wheel events one step of movement costs. Higher is
	// less sensitive. Set it to 1 if you scroll with a mouse wheel, where one
	// notch is one event.
	ScrollTicks int `toml:"scroll_ticks"`
	// ScrollIntervalMS is the minimum gap between two scroll steps. This is what
	// tames a trackpad: its momentum keeps firing wheel events long after your
	// fingers stop, and no per-event count can throttle that on its own. Set it
	// to 0 to remove the cap.
	ScrollIntervalMS *int `toml:"scroll_interval_ms"`
}

// Scroll defaults, matching ScrollPresets[DefaultScrollLevel].
const (
	DefaultScrollTicks      = 2
	DefaultScrollIntervalMS = 60
)

// ScrollPreset is one rung of the sensitivity ladder. Tuning two numbers by
// hand is guesswork, so the TUI steps through these instead.
type ScrollPreset struct {
	Ticks      int
	IntervalMS int
}

// ScrollPresets runs slowest to fastest. The last rung removes both limits, for
// a mouse wheel where one notch is one event and there is no momentum to tame.
var ScrollPresets = []ScrollPreset{
	{Ticks: 6, IntervalMS: 200},
	{Ticks: 4, IntervalMS: 140},
	{Ticks: 3, IntervalMS: 100},
	{Ticks: 2, IntervalMS: 60},
	{Ticks: 1, IntervalMS: 30},
	{Ticks: 1, IntervalMS: 0},
}

// DefaultScrollLevel indexes ScrollPresets.
const DefaultScrollLevel = 3

// StepsPerSecond is the ceiling this preset puts on cursor movement, or 0 when
// uncapped. It is what the sensitivity readout shows.
func (p ScrollPreset) StepsPerSecond() int {
	if p.IntervalMS <= 0 {
		return 0
	}
	return 1000 / p.IntervalMS
}

// NearestScrollLevel finds the rung closest to an explicit ticks/interval pair,
// so hand-edited config still lands somewhere sensible on the ladder.
func NearestScrollLevel(ticks, intervalMS int) int {
	best, bestDist := DefaultScrollLevel, -1
	for i, p := range ScrollPresets {
		// Weight the interval down so the two terms are comparable in size.
		dist := abs(p.Ticks-ticks)*20 + abs(p.IntervalMS-intervalMS)
		if bestDist < 0 || dist < bestDist {
			best, bestDist = i, dist
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// MouseEnabled reports whether to put the terminal into mouse-reporting mode.
// Absent from the config means enabled.
func (u UI) MouseEnabled() bool { return u.Mouse == nil || *u.Mouse }

// TicksPerStep returns the effective scroll sensitivity, guarding against a
// zero or negative value in the config file.
func (u UI) TicksPerStep() int {
	if u.ScrollTicks < 1 {
		return DefaultScrollTicks
	}
	return u.ScrollTicks
}

// ScrollInterval returns the minimum gap between scroll steps. Absent from the
// config means the default; an explicit 0 removes the cap.
func (u UI) ScrollInterval() time.Duration {
	ms := DefaultScrollIntervalMS
	if u.ScrollIntervalMS != nil {
		ms = *u.ScrollIntervalMS
	}
	if ms < 0 {
		ms = 0
	}
	return time.Duration(ms) * time.Millisecond
}

// Path returns the config file location, honouring XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("TOOLBOX_TUI_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.toml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "toolbox-tui", "config.toml")
}

// Load reads the config file and applies defaults. A missing file is an error
// with instructions, since we cannot guess which project to show.
func Load() (*Config, error) {
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found at %s\n\nRun `tt init` to write a starter config", path)
		}
		return nil, err
	}
	var c Config
	if _, err := toml.Decode(string(data), &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.applyDefaults()
	return &c, c.validate()
}

func (c *Config) applyDefaults() {
	if c.GitHub.OwnerType == "" {
		c.GitHub.OwnerType = "user"
	}
	if c.UI.RoadmapDays == 0 {
		c.UI.RoadmapDays = 42
	}
	if len(c.UI.StatusOrder) == 0 {
		c.UI.StatusOrder = []string{"Pending", "Todo", "In Progress", "In Review", "Done"}
	}
}

func (c *Config) validate() error {
	if c.GitHub.Owner == "" {
		return fmt.Errorf("github.owner is required")
	}
	if c.GitHub.ProjectNumber == 0 {
		return fmt.Errorf("github.project_number is required")
	}
	switch c.GitHub.OwnerType {
	case "user", "organization":
	default:
		return fmt.Errorf("github.owner_type must be \"user\" or \"organization\", got %q", c.GitHub.OwnerType)
	}
	if c.GitHub.DefaultRepo != "" && !strings.Contains(c.GitHub.DefaultRepo, "/") {
		return fmt.Errorf("github.default_repo must be \"owner/name\", got %q", c.GitHub.DefaultRepo)
	}
	return nil
}

// RepoParts splits DefaultRepo into owner and name.
func (c *Config) RepoParts() (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(c.GitHub.DefaultRepo, "/")
	if !ok || owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}

// ResolveToken finds a GitHub token: explicit config, then GITHUB_TOKEN /
// GH_TOKEN, then whatever `gh` has stored in its keyring.
func (c *Config) ResolveToken() (string, error) {
	if c.GitHub.Token != "" {
		return c.GitHub.Token, nil
	}
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := os.Getenv(env); v != "" {
			return v, nil
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf("no GitHub token found: set github.token in %s, export GITHUB_TOKEN, or run `gh auth login`", Path())
}

// ConfigPath returns the file this config was (or would be) loaded from. It is
// a method so views can point the user at their own path in error messages.
func (c *Config) ConfigPath() string { return Path() }
