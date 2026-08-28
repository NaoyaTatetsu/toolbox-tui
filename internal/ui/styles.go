package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Palette. Colours are adaptive so the TUI stays legible on light and dark
// terminals without asking the user to configure anything.
var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#3b5bdb", Dark: "#7aa2f7"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#868e96", Dark: "#6c7086"}
	colFg      = lipgloss.AdaptiveColor{Light: "#1e1e2e", Dark: "#cdd6f4"}
	colSubtle  = lipgloss.AdaptiveColor{Light: "#495057", Dark: "#9399b2"}
	colDanger  = lipgloss.AdaptiveColor{Light: "#c92a2a", Dark: "#f38ba8"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#e8590c", Dark: "#fab387"}
	colOK      = lipgloss.AdaptiveColor{Light: "#2b8a3e", Dark: "#a6e3a1"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#ced4da", Dark: "#45475a"}
	colSelBG   = lipgloss.AdaptiveColor{Light: "#dbe4ff", Dark: "#2a2b3c"}
	colTodayBG = lipgloss.AdaptiveColor{Light: "#fff3bf", Dark: "#3a3a2e"}
)

// ghColor maps GitHub's single-select palette names onto terminal colours.
func ghColor(name string) lipgloss.TerminalColor {
	switch strings.ToUpper(name) {
	case "RED":
		return lipgloss.AdaptiveColor{Light: "#c92a2a", Dark: "#f38ba8"}
	case "ORANGE":
		return lipgloss.AdaptiveColor{Light: "#e8590c", Dark: "#fab387"}
	case "YELLOW":
		return lipgloss.AdaptiveColor{Light: "#e67700", Dark: "#f9e2af"}
	case "GREEN":
		return lipgloss.AdaptiveColor{Light: "#2b8a3e", Dark: "#a6e3a1"}
	case "BLUE":
		return lipgloss.AdaptiveColor{Light: "#1971c2", Dark: "#89b4fa"}
	case "PURPLE":
		return lipgloss.AdaptiveColor{Light: "#6741d9", Dark: "#cba6f7"}
	case "PINK":
		return lipgloss.AdaptiveColor{Light: "#c2255c", Dark: "#f5c2e7"}
	case "GRAY", "GREY":
		return colMuted
	default:
		return colSubtle
	}
}

// hexColor turns a GitHub label colour ("5319e7", no '#') into a terminal
// colour, falling back to the muted grey when it is missing or malformed.
func hexColor(hex string) lipgloss.TerminalColor {
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
func priorityColor(p string) lipgloss.TerminalColor {
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
