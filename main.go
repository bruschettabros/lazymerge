package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazymerge: not inside a git repository")
		os.Exit(1)
	}
	if err := os.Chdir(strings.TrimSpace(root)); err != nil {
		fmt.Fprintln(os.Stderr, "lazymerge:", err)
		os.Exit(1)
	}
	m := model{theme: styles.Get("catppuccin-latte")}
	if lipgloss.HasDarkBackground() {
		m.theme = styles.Get("catppuccin-mocha")
	}
	m.refresh()
	m.preview()
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazymerge:", err)
		os.Exit(1)
	}
}
