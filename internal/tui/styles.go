package tui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha palette — https://catppuccin.com/palette/
//
// Theme mapping:
//
//	accent  → Mauve   (#cba6f7) - tabs, highlights, active focus
//	muted   → Overlay0 (#6c7086) - dim text, sub-labels
//	border  → Surface1 (#45475a) - panel + tab borders
//	ok      → Green   (#a6e3a1) - positive amounts, success flash
//	warn    → Peach   (#fab387) - caution, sinking-fund hints
//	bad     → Red     (#f38ba8) - negative amounts, errors
//	heading → Blue    (#89b4fa) - titles
//	onDark  → Base    (#1e1e2e) - text drawn on accent backgrounds
//	bgPanel → Surface0 (#313244) - status bar background
var (
	colorAccent  = lipgloss.Color("#cba6f7") // Mauve
	colorMuted   = lipgloss.Color("#6c7086") // Overlay0
	colorBorder  = lipgloss.Color("#45475a") // Surface1
	colorOK      = lipgloss.Color("#a6e3a1") // Green
	colorWarn    = lipgloss.Color("#fab387") // Peach
	colorBad     = lipgloss.Color("#f38ba8") // Red
	colorHeading = lipgloss.Color("#89b4fa") // Blue
	colorOnDark  = lipgloss.Color("#1e1e2e") // Base (used as fg on accent bg)
	colorBgPanel = lipgloss.Color("#313244") // Surface0

	activeTabBorder = lipgloss.Border{
		Top: "─", Bottom: " ", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "┘", BottomRight: "└",
	}
	inactiveTabBorder = lipgloss.Border{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "┴", BottomRight: "┴",
	}

	styleApp = lipgloss.NewStyle().Padding(0, 1)

	styleTab = lipgloss.NewStyle().
			Border(inactiveTabBorder, true).
			BorderForeground(colorBorder).
			Foreground(colorMuted).
			Padding(0, 2)

	styleTabActive = lipgloss.NewStyle().
			Border(activeTabBorder, true).
			BorderForeground(colorAccent).
			Foreground(colorAccent).
			Bold(true).
			Padding(0, 2)

	styleTabGap = lipgloss.NewStyle().
			Border(inactiveTabBorder, true).
			BorderForeground(colorBorder).
			BorderTop(false).
			BorderLeft(false).
			BorderRight(false)

	styleTitle = lipgloss.NewStyle().
			Foreground(colorHeading).
			Bold(true)

	styleHelp = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)

	styleHeader = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleDim = lipgloss.NewStyle().Foreground(colorMuted)
	styleErr = lipgloss.NewStyle().Foreground(colorBad).Bold(true)

	stylePos  = lipgloss.NewStyle().Foreground(colorOK)
	styleNeg  = lipgloss.NewStyle().Foreground(colorBad)
	styleWarn = lipgloss.NewStyle().Foreground(colorWarn)

	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	// Status bar (bottom).
	styleStatusBar = lipgloss.NewStyle().
			Background(colorBgPanel).
			Foreground(colorOnDark)

	styleStatusMode = lipgloss.NewStyle().
			Background(colorAccent).
			Foreground(colorOnDark).
			Padding(0, 1).
			Bold(true)

	styleStatusKeys = lipgloss.NewStyle().
			Background(colorBgPanel).
			Foreground(lipgloss.Color("250")).
			Padding(0, 1)

	styleStatusOK = lipgloss.NewStyle().
			Background(colorOK).
			Foreground(colorOnDark).
			Padding(0, 1).
			Bold(true)

	styleStatusErr = lipgloss.NewStyle().
			Background(colorBad).
			Foreground(colorOnDark).
			Padding(0, 1).
			Bold(true)
)
