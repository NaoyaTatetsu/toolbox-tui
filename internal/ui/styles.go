package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// darkBackground records what the terminal said about its own background.
// lipgloss v1 asked the terminal itself; v2 leaves the question to the program,
// which learns the answer from a tea.BackgroundColorMsg on start-up. Dark is
// the assumption until then, being the common case, and the wrong guess costs
// one frame.
var darkBackground = true

// adaptive is a colour with a value for each kind of terminal. It resolves when
// a style renders, so the palette can be package-level even though the answer
// arrives after start-up.
type adaptive struct{ light, dark color.Color }

func (a adaptive) RGBA() (r, g, b, alpha uint32) {
	if darkBackground {
		return a.dark.RGBA()
	}
	return a.light.RGBA()
}

// pair builds an adaptive colour from two hex strings.
func pair(light, dark string) adaptive {
	return adaptive{light: lipgloss.Color(light), dark: lipgloss.Color(dark)}
}

// Palette. Colours are adaptive so the TUI stays legible on light and dark
// terminals without asking the user to configure anything.
var (
	colAccent  = pair("#3b5bdb", "#7aa2f7")
	colMuted   = pair("#868e96", "#6c7086")
	colFg      = pair("#1e1e2e", "#cdd6f4")
	colSubtle  = pair("#495057", "#9399b2")
	colDanger  = pair("#c92a2a", "#f38ba8")
	colWarn    = pair("#e8590c", "#fab387")
	colOK      = pair("#2b8a3e", "#a6e3a1")
	colBorder  = pair("#ced4da", "#45475a")
	colSelBG   = pair("#dbe4ff", "#2a2b3c")
	colTodayBG = pair("#fff3bf", "#3a3a2e")

	// Weekend columns of the calendar, following the convention of a Japanese
	// wall calendar: Sunday in red, Saturday in blue.
	colSunday   = pair("#c92a2a", "#f38ba8")
	colSaturday = pair("#1971c2", "#89b4fa")
)

// ghColor maps GitHub's single-select palette names onto terminal colours.
func ghColor(name string) color.Color {
	switch strings.ToUpper(name) {
	case "RED":
		return pair("#c92a2a", "#f38ba8")
	case "ORANGE":
		return pair("#e8590c", "#fab387")
	case "YELLOW":
		return pair("#e67700", "#f9e2af")
	case "GREEN":
		return pair("#2b8a3e", "#a6e3a1")
	case "BLUE":
		return pair("#1971c2", "#89b4fa")
	case "PURPLE":
		return pair("#6741d9", "#cba6f7")
	case "PINK":
		return pair("#c2255c", "#f5c2e7")
	case "GRAY", "GREY":
		return colMuted
	default:
		return colSubtle
	}
}

// hexColor turns a GitHub label colour ("5319e7", no '#') into a terminal
// colour, falling back to the muted grey when it is missing or malformed.
func hexColor(hex string) color.Color {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return colMuted
	}
	for _, r := range hex {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return colMuted
		}
	}
	return lipgloss.Color("#" + hex)
}

// priorityColor keeps High/Middle/Low visually consistent across every view.
func priorityColor(p string) color.Color {
	switch strings.ToLower(p) {
	case "high", "urgent", "p0":
		return colDanger
	case "middle", "medium", "p1":
		return colWarn
	case "low", "p2":
		return colOK
	default:
		return colMuted
	}
}

// Shared styles.
var (
	styTitle    = lipgloss.NewStyle().Bold(true).Foreground(colFg)
	styMuted    = lipgloss.NewStyle().Foreground(colMuted)
	stySubtle   = lipgloss.NewStyle().Foreground(colSubtle)
	styAccent   = lipgloss.NewStyle().Foreground(colAccent)
	styDanger   = lipgloss.NewStyle().Foreground(colDanger)
	styOK       = lipgloss.NewStyle().Foreground(colOK)
	styTabOn    = lipgloss.NewStyle().Bold(true).Foreground(colAccent).Underline(true)
	styTabOff   = lipgloss.NewStyle().Foreground(colMuted)
	styKey      = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styStatus   = lipgloss.NewStyle().Foreground(colSubtle)
	styOverlay  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(1, 2)
	styColHead  = lipgloss.NewStyle().Bold(true)
	styCard     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	styCardSel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(0, 1)
	styFieldLbl = lipgloss.NewStyle().Foreground(colMuted).Width(11)
)

// overlayBox renders a panel whose padded interior is width cells across.
// lipgloss v2 counts the border inside Width, so the two border columns are
// added back here rather than at each of the four call sites.
func overlayBox(width int, body string) string {
	return styOverlay.Width(width + 2).Render(body)
}

// truncate shortens text to width cells, appending an ellipsis. It is
// grapheme- and East-Asian-width aware, which matters for Japanese titles.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", " ")
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// pad right-pads text to exactly width cells (truncating when too long).
func pad(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// center centres text within width cells.
func center(s string, width int) string {
	s = truncate(s, width)
	gap := width - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	left := gap / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", gap-left)
}

// keyHint renders "key desc" pairs for the footer and help overlay.
func keyHint(pairs ...[2]string) string {
	var parts []string
	for _, p := range pairs {
		parts = append(parts, styKey.Render(p[0])+" "+styMuted.Render(p[1]))
	}
	return strings.Join(parts, styMuted.Render("  ·  "))
}
