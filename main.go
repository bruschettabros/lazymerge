package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type ac = lipgloss.AdaptiveColor

// cp is a Catppuccin colour: Latte on light terminals, Mocha on dark ones.
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

	// Per pane: ours, result, theirs. Backgrounds are the pane colour
	// blended onto the Catppuccin base.
	paneFg   = [3]ac{cBlue, cMauve, cGreen}
	paneBg   = [3]ac{cp("#d0dcf5", "#2e344d"), cp("#e0d5f4", "#38324c"), cp("#d5e5d7", "#364143")}
	paneBgHi = [3]ac{cp("#b0c7f5", "#3e4b6b"), cp("#e0d5f4", "#38324c"), cp("#bbd9b8", "#495d53")}
)

func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func badge(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(cCrust).Bold(true).Padding(0, 1).Render(text)
}

type fileItem struct {
	path      string
	conflicts int // -1 when the file cannot be parsed
}

type model struct {
	files      []fileItem
	cursor     int
	file       *File
	lexer      chroma.Lexer
	theme      *chroma.Style
	cur        int // current chunk
	focus      int // Ours or Theirs
	scroll     int
	w, h       int
	msg        string
	msgErr     bool
	dirty      bool
	confirmEsc bool
}

type row struct {
	cells [3]string
	chunk int // -1 for plain lines
	first bool
	empty [3]bool // that pane has no lines for this chunk
}

// editedMsg arrives when the external editor exits.
type editedMsg struct {
	path   string
	reopen bool // go back to the merge view if conflicts remain
	err    error
}

func main() {
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(os.Stderr, "conflux: not inside a git repository")
		os.Exit(1)
	}
	if err := os.Chdir(strings.TrimSpace(root)); err != nil {
		fmt.Fprintln(os.Stderr, "conflux:", err)
		os.Exit(1)
	}
	m := model{theme: styles.Get("catppuccin-latte")}
	if lipgloss.HasDarkBackground() {
		m.theme = styles.Get("catppuccin-mocha")
	}
	m.refresh()
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "conflux:", err)
		os.Exit(1)
	}
}

func (m *model) refresh() {
	paths, err := ConflictedFiles()
	if err != nil {
		m.say(err.Error(), true)
	}
	m.files = nil
	for _, p := range paths {
		n := -1
		if data, err := os.ReadFile(p); err == nil {
			if segs, _, err := Parse(string(data)); err == nil {
				n = len((&File{Segs: segs}).Chunks())
			}
		}
		m.files = append(m.files, fileItem{p, n})
	}
	m.cursor = min(m.cursor, max(len(m.files)-1, 0))
}

func (m *model) say(msg string, isErr bool) { m.msg, m.msgErr = msg, isErr }

func (m *model) open(f *File) {
	m.file, m.focus, m.dirty = f, Ours, false
	m.lexer = lexers.Match(f.Path)
	if m.lexer != nil {
		m.lexer = chroma.Coalesce(m.lexer)
	}
	m.jumpTo(0)
}

// editCmd opens path in the editor git itself uses (GIT_EDITOR,
// core.editor, VISUAL, EDITOR, then vi), run through sh the same way git does.
func editCmd(path string, reopen bool) tea.Cmd {
	editor, err := git("var", "GIT_EDITOR")
	if err != nil {
		return func() tea.Msg { return editedMsg{path, reopen, err} }
	}
	editor = strings.TrimSpace(editor)
	cmd := exec.Command("sh", "-c", editor+` "$@"`, editor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{path, reopen, err} })
}

func (m model) afterEdit(e editedMsg) (tea.Model, tea.Cmd) {
	m.file = nil
	m.refresh()
	if e.err != nil {
		m.say("Editor failed: "+e.err.Error(), true)
		return m, nil
	}
	f, err := Load(e.path)
	switch {
	case errors.Is(err, ErrNoConflicts):
		if _, err := git("add", "--", e.path); err != nil {
			m.say(err.Error(), true)
			break
		}
		m.refresh()
		m.say("✓ "+e.path+" has no conflicts left, staged", false)
	case err != nil:
		m.say(e.path+": "+err.Error(), true)
	case e.reopen:
		m.open(f)
	}
	return m, nil
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case editedMsg:
		return m.afterEdit(msg)
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.file == nil {
			return m.updateList(msg)
		}
		return m.updateMerge(msg)
	}
	return m, nil
}

func (m model) updateList(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.say("", false)
	switch k.String() {
	case "q", "esc":
		return m, tea.Quit
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "j", "down":
		m.cursor = min(m.cursor+1, max(len(m.files)-1, 0))
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(len(m.files)-1, 0)
	case "r":
		m.refresh()
	case "e":
		if len(m.files) > 0 {
			return m, editCmd(m.files[m.cursor].path, false)
		}
	case "enter", "l", "right":
		if len(m.files) == 0 {
			break
		}
		path := m.files[m.cursor].path
		f, err := Load(path)
		if err != nil {
			m.say(path+": "+err.Error(), true)
			break
		}
		m.open(f)
	}
	return m, nil
}

func (m model) updateMerge(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	chunks := m.file.Chunks()
	c := chunks[m.cur]
	key := k.String()
	if key != "esc" && key != "q" {
		m.confirmEsc = false
	}
	m.say("", false)
	half := max(m.bodyHeight()/2, 1)
	switch key {
	case "esc", "q":
		if m.dirty && !m.confirmEsc {
			m.confirmEsc = true
			m.say("Unsaved changes. Press esc again to discard them.", true)
			break
		}
		m.file, m.confirmEsc = nil, false
		m.refresh()
		return m, nil
	case "j", "down":
		m.jumpTo(min(m.cur+1, len(chunks)-1))
	case "k", "up":
		m.jumpTo(max(m.cur-1, 0))
	case "g":
		m.jumpTo(0)
	case "G":
		m.jumpTo(len(chunks) - 1)
	case "h", "left":
		m.focus = Ours
	case "l", "right":
		m.focus = Theirs
	case "ctrl+d":
		m.scroll += half
	case "ctrl+u":
		m.scroll -= half
	case "ctrl+e":
		m.scroll++
	case "ctrl+y":
		m.scroll--
	case " ":
		c.Toggle(m.focus)
		m.dirty = true
	case "x":
		c.Decline(m.focus)
		m.dirty = true
	case "u":
		c.Reset()
		m.dirty = true
	case "n":
		for i := 1; i <= len(chunks); i++ {
			if j := (m.cur + i) % len(chunks); !chunks[j].Done() {
				m.jumpTo(j)
				break
			}
		}
	case "m":
		n := 0
		for _, ch := range chunks {
			if ch.AutoResolve() {
				n++
			}
		}
		m.dirty = m.dirty || n > 0
		m.say(fmt.Sprintf("✨ Auto-resolved %d of %d conflicts", n, len(chunks)), false)
	case "y":
		m.copy([...]string{"ours", "", "theirs"}[m.focus*2], c.Sides[m.focus])
	case "Y":
		m.copy("result", c.Result())
	case "e":
		// Keep the choices made so far: write them, with markers for the rest.
		if m.dirty {
			if err := m.file.WriteProgress(); err != nil {
				m.say("Could not write file: "+err.Error(), true)
				break
			}
			m.dirty = false
		}
		return m, editCmd(m.file.Path, true)
	case "s":
		if err := m.file.Save(); err != nil {
			m.say("Save failed: "+err.Error(), true)
			break
		}
		path := m.file.Path
		m.file = nil
		m.refresh()
		m.say("✓ "+path+" saved and staged", false)
		return m, nil
	}
	m.scroll = max(min(m.scroll, len(m.rows())-m.bodyHeight()), 0)
	return m, nil
}

func (m *model) copy(what string, lines []string) {
	if err := clipboard.WriteAll(strings.Join(lines, "\n")); err != nil {
		m.say("Copy failed: "+err.Error(), true)
		return
	}
	m.say(fmt.Sprintf("📋 Copied %s (%d lines)", what, len(lines)), false)
}

// jumpTo selects chunk i and scrolls so it is in view with some context.
func (m *model) jumpTo(i int) {
	m.cur = i
	for r, row := range m.rows() {
		if row.chunk == i {
			if r < m.scroll+2 || r > m.scroll+m.bodyHeight()-4 {
				m.scroll = max(r-m.bodyHeight()/3, 0)
			}
			return
		}
	}
}

// header 1 + pane borders 2 + pane title 1 + status 1 + help 1
func (m model) bodyHeight() int { return max(m.h-6, 1) }

func (m model) rows() []row {
	var rows []row
	ci := 0
	for _, s := range m.file.Segs {
		if s.Chunk == nil {
			for _, l := range s.Lines {
				rows = append(rows, row{cells: [3]string{l, l, l}, chunk: -1})
			}
			continue
		}
		cols := [3][]string{s.Chunk.Sides[Ours], s.Chunk.Result(), s.Chunk.Sides[Theirs]}
		n := max(len(cols[0]), len(cols[1]), len(cols[2]), 1)
		for i := 0; i < n; i++ {
			r := row{chunk: ci, first: i == 0}
			for p := range cols {
				if i < len(cols[p]) {
					r.cells[p] = cols[p][i]
				}
				r.empty[p] = len(cols[p]) == 0
			}
			rows = append(rows, r)
		}
		ci++
	}
	return rows
}

// paint renders one line at exactly width w on top of base, with syntax
// colours when highlight is set.
// shortcut: lexes each line alone, so multi-line strings/comments can be
// coloured wrong; lex whole files if that becomes a problem.
func (m model) paint(text string, w int, base lipgloss.Style, highlight bool) string {
	text = runewidth.Truncate(strings.ReplaceAll(text, "\t", "    "), w, "…")
	pad := base.Render(strings.Repeat(" ", max(w-runewidth.StringWidth(text), 0)))
	if !highlight || m.lexer == nil {
		return base.Render(text) + pad
	}
	it, err := m.lexer.Tokenise(nil, text)
	if err != nil {
		return base.Render(text) + pad
	}
	var b strings.Builder
	for _, tok := range it.Tokens() {
		st := base
		if e := m.theme.Get(tok.Type); e.Colour.IsSet() {
			st = st.Foreground(lipgloss.Color(e.Colour.String()))
		}
		b.WriteString(st.Render(strings.ReplaceAll(tok.Value, "\n", "")))
	}
	return b.String() + pad
}

func (m model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.file == nil {
		return m.viewList()
	}
	return m.viewMerge()
}

// helpBar takes key, description pairs. Each key gets the next rainbow colour.
func (m model) helpBar(keys ...string) string {
	var parts []string
	for i := 0; i+1 < len(keys); i += 2 {
		parts = append(parts, fg(rainbow[i/2%len(rainbow)]).Bold(true).Render(keys[i])+" "+fg(cSub).Render(keys[i+1]))
	}
	return lipgloss.NewStyle().MaxWidth(m.w).Render(" " + strings.Join(parts, fg(cSurface1).Render(" • ")))
}

func (m model) toast() string {
	if m.msg == "" {
		return ""
	}
	if m.msgErr {
		return fg(cRed).Bold(true).Render("✗ " + m.msg)
	}
	return fg(cGreen).Render(m.msg)
}

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(" " + badge("◆ conflux", cMauve) + fg(cLavender).Render("  merge conflict resolver") + "\n\n")
	boxW := max(min(m.w-2, 80), 24)
	inner := boxW - 4
	var lines []string
	if len(m.files) == 0 {
		lines = append(lines, fg(cGreen).Bold(true).Render("✓ No conflicted files. All clear! 🎉"))
	}
	for i, f := range m.files {
		count := fmt.Sprintf("%d conflict", f.conflicts)
		if f.conflicts != 1 {
			count += "s"
		}
		if f.conflicts < 0 {
			count = "cannot parse"
		}
		name := runewidth.Truncate(f.path, inner-len(count)-5, "…")
		gap := strings.Repeat(" ", max(inner-3-runewidth.StringWidth(name)-len(count), 1))
		if i == m.cursor {
			bg := lipgloss.NewStyle().Background(cSurface0)
			lines = append(lines, bg.Foreground(cPeach).Render("▌ ")+bg.Foreground(cText).Bold(true).Render(name)+
				bg.Render(gap)+bg.Foreground(cPeach).Render(count)+bg.Render(" "))
		} else {
			lines = append(lines, "  "+fg(cText).Render(name)+gap+fg(cOverlay).Render(count)+" ")
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMauve).
		Padding(0, 1).Width(boxW - 2).Render(strings.Join(lines, "\n"))
	b.WriteString(box + "\n " + m.toast() + "\n")
	b.WriteString(m.helpBar("j/k", "move", "enter", "open", "e", "edit", "r", "refresh", "q", "quit"))
	return b.String()
}

func (m model) viewMerge() string {
	chunks := m.file.Chunks()
	open := 0
	for _, c := range chunks {
		if !c.Done() {
			open++
		}
	}

	// Header: badge, path, rainbow progress bar.
	left := " " + badge("◆ conflux", cMauve) + " " + fg(cLavender).Bold(true).Render(m.file.Path)
	if m.dirty {
		left += fg(cPeach).Render(" ● unsaved")
	}
	done := len(chunks) - open
	barW := 20
	filled := done * barW / len(chunks)
	var bar strings.Builder
	for i := range barW {
		if i < filled {
			bar.WriteString(fg(rainbow[i*len(rainbow)/barW]).Render("━"))
		} else {
			bar.WriteString(fg(cSurface1).Render("━"))
		}
	}
	progress := fg(cSub).Render(fmt.Sprintf(" %d/%d resolved ", done, len(chunks)))
	if open == 0 {
		progress = fg(cGreen).Bold(true).Render(" ✓ all resolved, press s ")
	}
	right := bar.String() + progress
	header := left + strings.Repeat(" ", max(m.w-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right

	// Panes.
	rows := m.rows()
	bodyH := m.bodyHeight()
	start := min(m.scroll, max(len(rows)-1, 0))
	end := min(start+bodyH, len(rows))
	widths := [3]int{m.w / 3, m.w - 2*(m.w/3), m.w / 3}
	titles := [3]string{"◀ OURS · " + m.file.Labels[Ours], "✦ RESULT ✦", "THEIRS · " + m.file.Labels[Theirs] + " ▶"}
	var panes [3]string
	for p := range 3 {
		inner := max(widths[p]-2, 4)
		focused := p == m.focus*2
		title := runewidth.Truncate(titles[p], inner-2, "…")
		ts := fg(paneFg[p]).Bold(true).Render(" " + title + " ")
		if focused {
			ts = badge(title, paneFg[p])
		}
		lines := []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, ts)}
		for _, r := range rows[start:end] {
			lines = append(lines, m.cell(r, p, inner, chunks))
		}
		for len(lines) < bodyH+1 {
			lines = append(lines, strings.Repeat(" ", inner))
		}
		border := lipgloss.TerminalColor(cSurface1)
		if focused {
			border = paneFg[p]
		}
		panes[p] = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(strings.Join(lines, "\n"))
	}

	// Status line for the current conflict.
	c := chunks[m.cur]
	status := " " + badge(fmt.Sprintf("Conflict %d/%d", m.cur+1, len(chunks)), cPeach) + "  "
	if c.AutoMerged() {
		status += fg(cTeal).Render("✨ auto-merged line by line")
	} else {
		for side, name := range []string{"ours", "theirs"} {
			status += fg(paneFg[side*2]).Bold(true).Render(name) + " " + stateBadge(c.States[side]) + "   "
		}
	}
	status = strings.TrimRight(status, " ") + "   " + m.toast()

	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes[:]...) + "\n" + status + "\n" +
		m.helpBar("j/k", "change", "h/l", "side", "space", "accept", "x", "decline", "u", "reset",
			"n", "next open", "m", "auto", "e", "edit", "y/Y", "copy", "s", "save", "esc", "back")
}

func stateBadge(s State) string {
	switch s {
	case Applied:
		return fg(cGreen).Render("✓ accepted")
	case Ignored:
		return fg(cRed).Render("✗ declined")
	}
	return fg(cYellow).Render("● pending")
}

// cell renders pane p of one row: a 2-wide gutter and the text.
func (m model) cell(r row, p, inner int, chunks []*Chunk) string {
	w := inner - 2
	if r.chunk < 0 {
		return "  " + m.paint(r.cells[p], w, fg(cText), true)
	}
	c := chunks[r.chunk]
	cur := r.chunk == m.cur
	base := fg(cText).Background(paneBg[p])
	gutter, gutterFg, highlight := "▎", lipgloss.TerminalColor(paneFg[p]), true
	if cur {
		gutterFg = cPeach
	}
	if p == 1 {
		if c.Done() {
			base = fg(cText)
		}
	} else {
		side := p / 2
		switch c.States[side] {
		case Applied:
			base, highlight = fg(cTeal), false
			if r.first {
				gutter, gutterFg = "✓", cGreen
			}
		case Ignored:
			base, highlight = fg(cOverlay).Strikethrough(true), false
			if r.first {
				gutter, gutterFg = "✗", cRed
			}
		default:
			if r.first {
				gutter, gutterFg = "●", cYellow
			}
		}
		if cur && side == m.focus {
			base = base.Background(paneBgHi[p]).Bold(true)
			if r.first {
				gutter, gutterFg = "▶", cPeach
			}
		}
	}
	text := r.cells[p]
	if r.first && r.empty[p] {
		text, highlight = "(no lines)", false
		base = base.Foreground(cOverlay).Italic(true)
	}
	return fg(gutterFg).Render(gutter+" ") + m.paint(text, w, base, highlight)
}
