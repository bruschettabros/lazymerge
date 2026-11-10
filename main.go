package main

import (
	"fmt"
	"os"
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

var (
	accent   = ac{Light: "#D75F00", Dark: "#FFAF5F"}
	faint    = ac{Light: "#9E9E9E", Dark: "#6C6C6C"}
	textFg   = ac{Light: "#303030", Dark: "#D0D0D0"}
	doneFg   = ac{Light: "#558B2F", Dark: "#8FBF7F"}
	errFg    = ac{Light: "#C62828", Dark: "#E06C75"}
	resultFg = ac{Light: "#7B1FA2", Dark: "#C678DD"}
	resultBg = ac{Light: "#F6EDF8", Dark: "#2A2033"}
	// Per pane: ours, result, theirs.
	paneFg   = [3]ac{{Light: "#005FAF", Dark: "#61AFEF"}, resultFg, {Light: "#2E7D32", Dark: "#98C379"}}
	paneBg   = [3]ac{{Light: "#E8F1FC", Dark: "#1B2B3C"}, resultBg, {Light: "#EAF6EA", Dark: "#1C3022"}}
	paneBgHi = [3]ac{{Light: "#C6DCF7", Dark: "#2A4D70"}, resultBg, {Light: "#C8E6C9", Dark: "#2E5236"}}

	faintStyle = lipgloss.NewStyle().Foreground(faint)
	keyStyle   = lipgloss.NewStyle().Foreground(accent).Bold(true)
	badgeStyle = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1C1C1C")).Bold(true).Padding(0, 1)
)

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
	m := model{theme: styles.Get("github")}
	if lipgloss.HasDarkBackground() {
		m.theme = styles.Get("monokai")
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

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
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
		m.file, m.focus, m.dirty = f, Ours, false
		m.lexer = lexers.Match(path)
		if m.lexer != nil {
			m.lexer = chroma.Coalesce(m.lexer)
		}
		m.jumpTo(0)
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
	m.say(fmt.Sprintf("Copied %s (%d lines)", what, len(lines)), false)
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

func (m model) helpBar(keys ...string) string {
	var parts []string
	for i := 0; i+1 < len(keys); i += 2 {
		parts = append(parts, keyStyle.Render(keys[i])+" "+faintStyle.Render(keys[i+1]))
	}
	return lipgloss.NewStyle().MaxWidth(m.w).Render(" " + strings.Join(parts, faintStyle.Render("  ·  ")))
}

func (m model) toast() string {
	if m.msg == "" {
		return ""
	}
	fg := lipgloss.TerminalColor(doneFg)
	if m.msgErr {
		fg = errFg
	}
	return lipgloss.NewStyle().Foreground(fg).Render(m.msg)
}

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(badgeStyle.Render("conflux") + faintStyle.Render("  merge conflict resolver") + "\n\n")
	boxW := max(min(m.w-2, 80), 20)
	var lines []string
	if len(m.files) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(doneFg).Render("✓ No conflicted files. All clear!"))
	}
	for i, f := range m.files {
		count := fmt.Sprintf("%d conflict", f.conflicts)
		if f.conflicts != 1 {
			count += "s"
		}
		if f.conflicts < 0 {
			count = "cannot parse"
		}
		name := runewidth.Truncate(f.path, boxW-len(count)-8, "…")
		gap := strings.Repeat(" ", max(boxW-6-runewidth.StringWidth(name)-len(count), 1))
		if i == m.cursor {
			lines = append(lines, keyStyle.Render("▌ ")+lipgloss.NewStyle().Bold(true).Render(name)+gap+keyStyle.Render(count))
		} else {
			lines = append(lines, "  "+name+gap+faintStyle.Render(count))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(faint).
		Padding(0, 1).Width(boxW - 2).Render(strings.Join(lines, "\n"))
	b.WriteString(box + "\n " + m.toast() + "\n")
	b.WriteString(m.helpBar("j/k", "move", "enter", "open", "r", "refresh", "q", "quit"))
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

	// Header: badge, path, progress bar.
	left := badgeStyle.Render("conflux") + " " + lipgloss.NewStyle().Bold(true).Render(m.file.Path)
	if m.dirty {
		left += keyStyle.Render(" ●")
	}
	done := len(chunks) - open
	barW := 12
	filled := done * barW / len(chunks)
	right := lipgloss.NewStyle().Foreground(doneFg).Render(strings.Repeat("━", filled)) +
		faintStyle.Render(strings.Repeat("━", barW-filled)) +
		fmt.Sprintf(" %d/%d resolved ", done, len(chunks))
	header := left + strings.Repeat(" ", max(m.w-lipgloss.Width(left)-lipgloss.Width(right), 1)) + right

	// Panes.
	rows := m.rows()
	bodyH := m.bodyHeight()
	start := min(m.scroll, max(len(rows)-1, 0))
	end := min(start+bodyH, len(rows))
	widths := [3]int{m.w / 3, m.w - 2*(m.w/3), m.w / 3}
	titles := [3]string{"◀ OURS · " + m.file.Labels[Ours], "RESULT", "THEIRS · " + m.file.Labels[Theirs] + " ▶"}
	var panes [3]string
	for p := range 3 {
		inner := max(widths[p]-2, 4)
		focused := p == m.focus*2
		ts := lipgloss.NewStyle().Foreground(paneFg[p]).Bold(true)
		if focused {
			ts = ts.Reverse(true)
		}
		lines := []string{lipgloss.PlaceHorizontal(inner, lipgloss.Center, ts.Render(" "+runewidth.Truncate(titles[p], inner-2, "…")+" "))}
		for _, r := range rows[start:end] {
			lines = append(lines, m.cell(r, p, inner, chunks))
		}
		for len(lines) < bodyH+1 {
			lines = append(lines, strings.Repeat(" ", inner))
		}
		border := lipgloss.TerminalColor(faint)
		if focused {
			border = accent
		}
		panes[p] = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(strings.Join(lines, "\n"))
	}

	// Status line for the current conflict.
	c := chunks[m.cur]
	status := keyStyle.Render(fmt.Sprintf(" Conflict %d/%d", m.cur+1, len(chunks))) + "  "
	if c.AutoMerged() {
		status += lipgloss.NewStyle().Foreground(doneFg).Render("✨ auto-merged line by line")
	} else {
		for side, name := range []string{"ours", "theirs"} {
			status += lipgloss.NewStyle().Foreground(paneFg[side*2]).Render(name) + " " + stateBadge(c.States[side]) + "   "
		}
	}
	status = strings.TrimRight(status, " ") + "   " + m.toast()

	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes[:]...) + "\n" + status + "\n" +
		m.helpBar("j/k", "change", "h/l", "side", "space", "accept", "x", "decline", "u", "reset",
			"n", "next open", "m", "auto", "y/Y", "copy side/result", "s", "save", "esc", "back")
}

func stateBadge(s State) string {
	switch s {
	case Applied:
		return lipgloss.NewStyle().Foreground(doneFg).Render("✓ accepted")
	case Ignored:
		return faintStyle.Render("✗ declined")
	}
	return lipgloss.NewStyle().Foreground(accent).Render("● pending")
}

// cell renders pane p of one row: a 2-wide gutter and the text.
func (m model) cell(r row, p, inner int, chunks []*Chunk) string {
	w := inner - 2
	if r.chunk < 0 {
		return "  " + m.paint(r.cells[p], w, lipgloss.NewStyle().Foreground(textFg), true)
	}
	c := chunks[r.chunk]
	cur := r.chunk == m.cur
	base := lipgloss.NewStyle().Foreground(textFg).Background(paneBg[p])
	gutter, highlight := "▎", true
	if p == 1 {
		if c.Done() {
			base = lipgloss.NewStyle().Foreground(textFg)
		}
	} else {
		side := p / 2
		switch c.States[side] {
		case Applied:
			base, highlight = lipgloss.NewStyle().Foreground(doneFg), false
			if r.first {
				gutter = "✓"
			}
		case Ignored:
			base, highlight = lipgloss.NewStyle().Foreground(faint).Strikethrough(true), false
			if r.first {
				gutter = "✗"
			}
		default:
			if r.first {
				gutter = "●"
			}
		}
		if cur && side == m.focus {
			base = base.Background(paneBgHi[p]).Bold(true)
			if r.first {
				gutter = "▶"
			}
		}
	}
	gfg := lipgloss.TerminalColor(paneFg[p])
	if cur {
		gfg = accent
	}
	text := r.cells[p]
	if r.first && r.empty[p] {
		text, highlight = "(no lines)", false
		base = base.Foreground(faint).Italic(true)
	}
	return lipgloss.NewStyle().Foreground(gfg).Render(gutter+" ") + m.paint(text, w, base, highlight)
}
