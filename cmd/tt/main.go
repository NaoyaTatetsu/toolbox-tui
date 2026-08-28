// Command tt is a terminal task manager over a GitHub Project board and Google
// Calendar feeds.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/NaoyaTatetsu/task-tui/internal/cache"
	"github.com/NaoyaTatetsu/task-tui/internal/config"
	gh "github.com/NaoyaTatetsu/task-tui/internal/github"
	googlecalendar "github.com/NaoyaTatetsu/task-tui/internal/google-calendar"
	"github.com/NaoyaTatetsu/task-tui/internal/ui"
)

const usage = `tt — GitHub Project board + Google Calendar in your terminal

usage:
  tt              open the TUI
  tt init         write a starter config file
  tt doctor       check config, token scopes, and connectivity
  tt cache clear  drop the on-disk cache used for the first frame
  tt version      show the build this binary came from
  tt help         show this message

config: ` + "`$XDG_CONFIG_HOME/task-tui/config.toml`" + ` (override with $TASK_TUI_CONFIG)
cache:  ` + "`$XDG_CACHE_HOME/task-tui/`" + ` (override with $TASK_TUI_CACHE)
`

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "":
		err = run()
	case "init":
		err = initConfig()
	case "doctor":
		err = doctor()
	case "cache":
		err = cacheCmd(os.Args[2:])
	case "version":
		printVersion()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func clients(cfg *config.Config) (*gh.Client, *googlecalendar.Client, error) {
	token, err := cfg.ResolveToken()
	if err != nil {
		return nil, nil, err
	}
	return gh.New(token), googlecalendar.NewClient(5 * time.Minute), nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ghClient, calendarClient, err := clients(cfg)
	if err != nil {
		return err
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.UI.MouseEnabled() {
		// Cell motion is the lightest mode that still reports wheel events.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(ui.New(cfg, ghClient, calendarClient), opts...)
	_, err = p.Run()
	return err
}

const starterConfig = `# task-tui configuration

[github]
# From the project URL: https://github.com/users/<owner>/projects/<number>
owner          = "%s"
owner_type     = "user"          # "user" or "organization"
project_number = 4

# New tasks are filed as issues here, then added to the project.
# Labels live on issues, so registering a task with labels needs this set.
default_repo   = ""              # e.g. "%s/notes"

# The token comes from $GITHUB_TOKEN, or ` + "`gh auth token`" + `, unless set here.
# Writing to a project needs the ` + "`project`" + ` scope:
#     gh auth refresh -s project,repo
# token = "ghp_…"

[ui]
# Left-to-right order of the board columns.
status_order = ["Pending", "Todo", "In Progress", "In Review", "Done"]
hide_done    = false
roadmap_days = 28

# Trackpad / wheel scrolling. Set to false to get the terminal's own
# click-and-drag text selection back.
mouse = true
# Scroll sensitivity. Rather than guessing these, press , and . in the TUI to
# step through the presets — the status line shows the line to paste back here.
scroll_ticks       = 2
scroll_interval_ms = 60

# Google Calendar: Settings → <your calendar> → "Secret address in iCal format".
# Repeat the block for each calendar you want to see.
# [[calendar.sources]]
# name  = "personal"
# url   = "https://calendar.google.com/calendar/ical/…/private-…/basic.ics"
# color = "#7aa2f7"
`

func initConfig() error {
	path := config.Path()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; edit it or delete it first", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	owner := "your-github-login"
	if login, err := currentLogin(); err == nil && login != "" {
		owner = login
	}
	body := fmt.Sprintf(starterConfig, owner, owner)
	// 0600: the file holds private calendar URLs and possibly a token.
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n\nEdit it, then run `tt doctor` to verify the setup.\n", path)
	return nil
}

// currentLogin asks gh who is authenticated, to pre-fill the starter config.
func currentLogin() (string, error) {
	cfg := &config.Config{}
	token, err := cfg.ResolveToken()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return gh.New(token).Login(ctx)
}

// buildStamp is set with -ldflags "-X main.buildStamp=…" by the Makefile. It
// exists so a stale binary earlier on $PATH is easy to spot: `which tt` plus
// `tt version` tells you immediately whether you are running today's build.
var buildStamp = "unknown"

func printVersion() {
	fmt.Printf("tt\n  build   %s\n", buildStamp)
	if exe, err := os.Executable(); err == nil {
		fmt.Printf("  binary  %s\n", exe)
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Printf("  module  %s\n  go      %s\n", info.Main.Path, info.GoVersion)
	}
}

func cacheCmd(args []string) error {
	if len(args) != 1 || args[0] != "clear" {
		return fmt.Errorf("usage: tt cache clear")
	}
	if err := cache.Clear(); err != nil {
		return err
	}
	fmt.Printf("cleared %s\n", cache.Dir())
	return nil
}

func doctor() error {
	ok := func(format string, a ...any) { fmt.Printf("  ✓ "+format+"\n", a...) }
	bad := func(format string, a ...any) { fmt.Printf("  ✗ "+format+"\n", a...) }
	warn := func(format string, a ...any) { fmt.Printf("  ! "+format+"\n", a...) }

	if exe, exeErr := os.Executable(); exeErr == nil {
		fmt.Printf("binary\n  ✓ %s (build %s)\n\n", exe, buildStamp)
	}

	fmt.Println("config")
	cfg, err := config.Load()
	if err != nil {
		bad("%v", err)
		return nil
	}
	ok("%s", config.Path())

	fmt.Println("\ngithub")
	token, err := cfg.ResolveToken()
	if err != nil {
		bad("%v", err)
		return nil
	}
	ok("token found")
	client := gh.New(token)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	scopes, err := client.Scopes(ctx)
	switch {
	case err != nil:
		warn("could not read token scopes: %v", err)
	case scopes == nil:
		warn("token reports no classic scopes (fine-grained PAT or app token)")
	default:
		ok("scopes: %s", strings.Join(scopes, ", "))
		if !gh.HasScope(scopes, "read:project") && !gh.HasScope(scopes, "project") {
			bad("missing read access to projects — run: gh auth refresh -s read:project")
		}
		if !gh.HasScope(scopes, "project") {
			warn("no `project` scope: the board is read-only.")
			warn("  registering tasks and moving cards needs: gh auth refresh -s project")
		}
		if cfg.GitHub.DefaultRepo != "" && !gh.HasScope(scopes, "repo") {
			warn("no `repo` scope: creating issues will fail")
		}
	}

	project, err := client.FetchProject(ctx, cfg.GitHub.OwnerType, cfg.GitHub.Owner, cfg.GitHub.ProjectNumber)
	if err != nil {
		bad("project: %v", err)
	} else {
		ok("project %q: %d items, %d fields", project.Title, len(project.Items), len(project.Fields))
		for _, want := range []string{gh.FieldStatus, gh.FieldPriority, gh.FieldStartDate, gh.FieldEndDate} {
			if f, found := project.Field(want); found {
				if len(f.Options) > 0 {
					names := make([]string, 0, len(f.Options))
					for _, o := range f.Options {
						names = append(names, o.Name)
					}
					ok("field %-11s %s", want, strings.Join(names, " / "))
				} else {
					ok("field %-11s %s", want, f.DataType)
				}
			} else {
				warn("field %-11s missing — that part of the UI will be empty", want)
			}
		}
	}

	if owner, name, valid := cfg.RepoParts(); !valid {
		warn("github.default_repo is unset — task registration is disabled")
	} else if repo, err := client.FetchRepo(ctx, owner, name); err != nil {
		bad("repo %s/%s: %v", owner, name, err)
	} else {
		ok("repo %s/%s: %d labels", owner, name, len(repo.Labels))
	}

	fmt.Println("\ncache")
	entries, cacheErr := os.ReadDir(cache.Dir())
	switch {
	case cacheErr != nil:
		warn("%s is empty — the first frame of a cold start will be a spinner", cache.Dir())
	default:
		ok("%s", cache.Dir())
		for _, e := range entries {
			info, statErr := e.Info()
			if statErr != nil {
				continue
			}
			ok("  %-28s %6d KB  %s", e.Name(), info.Size()/1024,
				info.ModTime().Format("2006-01-02 15:04"))
		}
		if len(entries) == 0 {
			warn("  no entries yet")
		}
	}

	fmt.Println("\ncalendar")
	if len(cfg.Calendar.Sources) == 0 {
		warn("no [[calendar.sources]] configured — the Calendar view will be empty")
		return nil
	}
	calendarClient := googlecalendar.NewClient(time.Minute)
	now := time.Now()
	for _, s := range cfg.Calendar.Sources {
		src := googlecalendar.Source{Name: s.Name, URL: s.URL, Color: s.Color}
		evs, errs := calendarClient.Events(ctx, []googlecalendar.Source{src}, now.AddDate(0, -1, 0), now.AddDate(0, 3, 0), time.Local)
		if len(errs) > 0 {
			for _, e := range errs {
				bad("%v", e)
			}
			continue
		}
		ok("%s: %d events in the next 3 months", s.Name, len(evs))
	}
	return nil
}
