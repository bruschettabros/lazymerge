package main

import (
	"github.com/charmbracelet/lipgloss"
)

type ac = lipgloss.AdaptiveColor

func cp(latte, mocha string) ac { return ac{Light: latte, Dark: mocha} }

var (
	cText     = cp("#4c4f69", "#cdd6f4")
	cSub      = cp("#6c6f85", "#a6adc8")
	cOverlay  = cp("#9ca0b0", "#6c7086")
	cSurface0 = cp("#ccd0da", "#313244")
	cSurface1 = cp("#bcc0cc", "#45475a")
	cCrust    = cp("#dce0e8", "#11111b")
	cRed      = cp("#d20f39", "#f38ba8")
	cPeach    = cp("#fe640b", "#fab387")
	cYellow   = cp("#df8e1d", "#f9e2af")
	cGreen    = cp("#40a02b", "#a6e3a1")
	cTeal     = cp("#179299", "#94e2d5")
	cSky      = cp("#04a5e5", "#89dceb")
	cBlue     = cp("#1e66f5", "#89b4fa")
	cLavender = cp("#7287fd", "#b4befe")
	cMauve    = cp("#8839ef", "#cba6f7")
	cPink     = cp("#ea76cb", "#f5c2e7")
	rainbow   = []ac{cMauve, cPink, cRed, cPeach, cYellow, cGreen, cTeal, cSky, cBlue, cLavender}

	paneFg   = [3]ac{cBlue, cMauve, cGreen}
	paneBg   = [3]ac{cp("#d0dcf5", "#2e344d"), cp("#e0d5f4", "#38324c"), cp("#d5e5d7", "#364143")}
	paneBgHi = [3]ac{cp("#b0c7f5", "#3e4b6b"), cp("#e0d5f4", "#38324c"), cp("#bbd9b8", "#495d53")}

	choiceFg = map[string]ac{"pending": cYellow, "ours": cBlue, "theirs": cGreen, "both": cMauve, "base": cOverlay, "auto-merged": cTeal}
)

func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func badge(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(cCrust).Bold(true).Padding(0, 1).Render(text)
}
