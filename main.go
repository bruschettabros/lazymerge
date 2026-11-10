package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

	// Per merge pane: ours, result, theirs. Backgrounds are the pane colour
	// blended onto the Catppuccin base.
	paneFg   = [3]ac{cBlue, cMauve, cGreen}
	paneBg   = [3]ac{cp("#d0dcf5", "#2e344d"), cp("#e0d5f4", "#38324c"), cp("#d5e5d7", "#364143")}
	paneBgHi = [3]ac{cp("#b0c7f5", "#3e4b6b"), cp("#e0d5f4", "#38324c"), cp("#bbd9b8", "#495d53")}

	choiceFg = map[string]ac{"pending": cYellow, "ours": cBlue, "theirs": cGreen, "both": cMauve, "base": cOverlay, "auto-merged": cTeal}
)

func fg(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

func badge(text string, bg lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Background(bg).Foreground(cCrust).Bold(true).Padding(0, 1).Render(text)
}

type panel int

const (
	pFiles panel = iota
	pConflicts
	pMerge
)

type fileItem struct {
	path      string
	conflicts int // -1 when the file cannot be parsed
}

type confirm struct {
	title, body string
	yes         func(m *model) tea.Cmd
}

type model struct {
	files   []fileItem
	cursor  int // selection in the Files panel
	file    *File
	loadErr string
	lexer   chroma.Lexer
	theme   *chroma.Style
	panel   panel
	cur     int // current chunk
	focus   int // Ours or Theirs
	scroll  int
	w, h    int
	msg     string
	msgErr  bool
	undo    [][]Chunk // chunk snapshots; non-empty means unsaved changes
	help    bool
	helpTop int // scroll position of the keybindings popup
	confirm *confirm
}

type row struct {
	cells [3]string
	chunk int // -1 for plain lines
	first bool
	empty [3]bool // that pane has no lines for this chunk
}

// editedMsg arrives when the external editor exits.
type editedMsg struct {
	path string
	err  error
}

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

// ---- state ----

func (m model) dirty() bool { return len(m.undo) > 0 }

func (m *model) say(msg string, isErr bool) { m.msg, m.msgErr = msg, isErr }

func (m *model) ask(title, body string, yes func(m *model) tea.Cmd) {
	m.confirm = &confirm{title, body, yes}
}

func (m *model) refresh() {
	paths, err := ConflictedFiles()
	if err != nil {
		m.say(err.Error(), true)
	}
	m.files = nil
	for _, p := range paths {
		n := -1 // same count as the Conflicts panel shows
		if f, err := Load(p); err == nil {
			n = len(f.Chunks())
		}
		m.files = append(m.files, fileItem{p, n})
	}
	m.cursor = min(m.cursor, max(len(m.files)-1, 0))
}

// preview shows the file under the cursor, like lazygit's main view. It never
// replaces a file with unsaved changes.
func (m *model) preview() {
	if m.dirty() {
		return
	}
	if len(m.files) == 0 {
		m.file, m.loadErr = nil, ""
		return
	}
	if path := m.files[m.cursor].path; m.file == nil || m.file.Path != path {
		m.load(path)
	}
}

func (m *model) load(path string) {
	m.file, m.loadErr, m.undo, m.cur, m.scroll, m.focus = nil, "", nil, 0, 0, Ours
	f, err := Load(path)
	if err != nil {
		m.loadErr = path + ": " + err.Error()
		return
	}
	m.file = f
	m.lexer = lexers.Match(path)
	if m.lexer != nil {
		m.lexer = chroma.Coalesce(m.lexer)
	}
	m.jumpTo(0)
}

func (m *model) openSelected() {
	if len(m.files) == 0 {
		return
	}
	path := m.files[m.cursor].path
	switch {
	case m.file != nil && m.file.Path == path:
	case m.dirty():
		m.ask("Discard changes?", fmt.Sprintf("You have unsaved changes in %s. Discard them and open %s?", m.file.Path, path),
			func(m *model) tea.Cmd {
				m.load(path)
				m.panel = pMerge
				return nil
			})
		return
	default:
		m.load(path)
	}
	if m.file != nil {
		m.panel = pMerge
	}
}

func (m *model) snapshot() {
	var s []Chunk
	for _, c := range m.file.Chunks() {
		cc := *c
		cc.applied = slices.Clone(c.applied)
		s = append(s, cc)
	}
	m.undo = append(m.undo, s)
}

func (m *model) popUndo() bool {
	if len(m.undo) == 0 {
		return false
	}
	s := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	for i, c := range m.file.Chunks() {
		*c = s[i]
	}
	return true
}

func (m *model) save() {
	if err := m.file.Save(); err != nil {
		m.say("Save failed: "+err.Error(), true)
		return
	}
	path := m.file.Path
	m.undo, m.file = nil, nil
	m.refresh()
	m.preview()
	if m.file == nil {
		m.panel = pFiles
	}
	m.say("✓ "+path+" saved and staged", false)
}

// editCmd opens path in the editor git itself uses (GIT_EDITOR,
// core.editor, VISUAL, EDITOR, then vi), run through sh the same way git does.
func editCmd(path string) tea.Cmd {
	editor, err := git("var", "GIT_EDITOR")
	if err != nil {
		return func() tea.Msg { return editedMsg{path, err} }
	}
	editor = strings.TrimSpace(editor)
	cmd := exec.Command("sh", "-c", editor+` "$@"`, editor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{path, err} })
}

func (m *model) edit(path string) tea.Cmd {
	// Keep the choices made so far: write them, with markers for the rest.
	if m.file != nil && m.file.Path == path && m.dirty() {
		if err := m.file.WriteProgress(); err != nil {
			m.say("Could not write file: "+err.Error(), true)
			return nil
		}
	}
	return editCmd(path)
}

func (m model) afterEdit(e editedMsg) (tea.Model, tea.Cmd) {
	if e.err != nil {
		m.say("Editor failed: "+e.err.Error(), true)
		return m, nil
	}
	if _, err := Load(e.path); errors.Is(err, ErrNoConflicts) {
		if _, err := git("add", "--", e.path); err != nil {
			m.say(err.Error(), true)
		} else {
			m.say("✓ "+e.path+" has no conflicts left, staged", false)
		}
	}
	m.refresh()
	if m.dirty() && m.file.Path != e.path {
		return m, nil // unsaved work in another file stays open
	}
	m.undo, m.file = nil, nil
	if i := slices.IndexFunc(m.files, func(f fileItem) bool { return f.path == e.path }); i >= 0 {
		m.cursor = i
	}
	m.preview()
	if m.file == nil {
		m.panel = pFiles
	}
	return m, nil
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

func (m *model) clampScroll() {
	if m.file != nil {
		m.scroll = max(min(m.scroll, len(m.rows())-m.bodyHeight()), 0)
	}
}

func (m *model) copy(what string, lines []string) {
	if err := clipboard.WriteAll(strings.Join(lines, "\n")); err != nil {
		m.say("Copy failed: "+err.Error(), true)
		return
	}
	m.say(fmt.Sprintf("📋 Copied %s (%d lines)", what, len(lines)), false)
}

// ---- input ----

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampScroll()
	case editedMsg:
		return m.afterEdit(msg)
	case tea.MouseMsg:
		m.mouse(msg)
	case tea.KeyMsg:
		return m.key(msg.String())
	}
	return m, nil
}

func (m model) key(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.confirm != nil {
		switch key {
		case "enter", "y":
			c := m.confirm
			m.confirm = nil
			return m, c.yes(&m)
		case "esc", "n", "q":
			m.confirm = nil
		}
		return m, nil
	}
	if m.help {
		switch key {
		case "esc", "q", "?", "enter":
			m.help = false
		case "j", "down":
			m.helpTop++
		case "k", "up":
			m.helpTop = max(m.helpTop-1, 0)
		}
		return m, nil
	}
	m.say("", false)
	switch key {
	case "q":
		if m.dirty() {
			m.ask("Quit?", "You have unsaved changes in "+m.file.Path+". Quit and discard them?",
				func(*model) tea.Cmd { return tea.Quit })
			return m, nil
		}
		return m, tea.Quit
	case "?":
		m.help, m.helpTop = true, 0
		return m, nil
	case "1":
		m.panel = pFiles
		return m, nil
	case "2", "3":
		if m.file != nil {
			m.panel = panel(key[0] - '1')
		}
		return m, nil
	case "tab", "shift+tab":
		if m.file != nil {
			d := 1
			if key == "shift+tab" {
				d = 2
			}
			m.panel = (m.panel + panel(d)) % 3
		}
		return m, nil
	case "esc":
		m.panel = pFiles
		return m, nil
	case "R", "r":
		m.refresh()
		if !m.dirty() {
			m.file = nil
			m.preview()
		}
		m.say("Refreshed", false)
		return m, nil
	case "e":
		if m.panel == pFiles && len(m.files) > 0 {
			return m, m.edit(m.files[m.cursor].path)
		}
		if m.file != nil {
			return m, m.edit(m.file.Path)
		}
		return m, nil
	}
	if m.panel == pFiles {
		m.keyFiles(key)
	} else if m.file != nil {
		m.keyMerge(key)
	}
	return m, nil
}

func (m *model) keyFiles(key string) {
	n := len(m.files)
	switch key {
	case "j", "down":
		m.cursor = min(m.cursor+1, max(n-1, 0))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(n-1, 0)
	case "enter", "l", "right", " ":
		m.openSelected()
		return
	default:
		return
	}
	m.preview()
}

func (m *model) keyMerge(key string) {
	chunks := m.file.Chunks()
	c := chunks[m.cur]
	half := max(m.bodyHeight()/2, 1)
	switch key {
	case "j", "down":
		m.jumpTo(min(m.cur+1, len(chunks)-1))
	case "k", "up":
		m.jumpTo(max(m.cur-1, 0))
	case "g", "home":
		m.jumpTo(0)
	case "G", "end":
		m.jumpTo(len(chunks) - 1)
	case "h", "left":
		m.focus = Ours
	case "l", "right":
		m.focus = Theirs
	case "enter":
		m.panel = pMerge
	case "ctrl+d":
		m.scroll += half
	case "ctrl+u":
		m.scroll -= half
	case "ctrl+e":
		m.scroll++
	case "ctrl+y":
		m.scroll--
	case " ":
		m.snapshot()
		c.Toggle(m.focus)
	case "b":
		m.snapshot()
		c.Reset()
		c.Accept(Ours)
		c.Accept(Theirs)
	case "x":
		m.snapshot()
		c.Decline(m.focus)
	case "z":
		if m.popUndo() {
			m.say("↶ Undone", false)
		} else {
			m.say("Nothing to undo", true)
		}
	case "n":
		for i := 1; i <= len(chunks); i++ {
			if j := (m.cur + i) % len(chunks); !chunks[j].Done() {
				m.jumpTo(j)
				break
			}
		}
	case "m":
		m.snapshot()
		n := 0
		for _, ch := range chunks {
			if ch.AutoResolve() {
				n++
			}
		}
		if n == 0 {
			m.undo = m.undo[:len(m.undo)-1]
		}
		m.say(fmt.Sprintf("✨ Auto-resolved %d of %d conflicts", n, len(chunks)), false)
	case "y":
		m.copy([...]string{"ours", "", "theirs"}[m.focus*2], c.Sides[m.focus])
	case "Y":
		m.copy("result", c.Result())
	case "s":
		m.save()
		return
	}
	m.clampScroll()
}

func (m *model) mouse(e tea.MouseMsg) {
	if m.confirm != nil || m.help {
		return
	}
	sideW, filesH := m.layout()
	inSide := e.X < sideW
	switch e.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		d := 1
		if e.Button == tea.MouseButtonWheelUp {
			d = -1
		}
		switch {
		case inSide && e.Y < filesH:
			m.cursor = max(min(m.cursor+d, len(m.files)-1), 0)
			m.preview()
		case inSide && m.file != nil:
			m.jumpTo(max(min(m.cur+d, len(m.file.Chunks())-1), 0))
		case m.file != nil:
			m.scroll += 3 * d
			m.clampScroll()
		}
		return
	case tea.MouseButtonLeft:
		if e.Action != tea.MouseActionPress {
			return
		}
	default:
		return
	}
	switch {
	case inSide && e.Y < filesH:
		m.panel = pFiles
		if i := listOffset(m.cursor, len(m.files), filesH-2) + e.Y - 1; e.Y > 0 && i >= 0 && i < len(m.files) {
			m.cursor = i
			m.preview()
		}
	case inSide && m.file != nil:
		m.panel = pConflicts
		n, ih := len(m.file.Chunks()), m.h-1-filesH-2
		if i := listOffset(m.cur, n, ih) + e.Y - filesH - 1; e.Y > filesH && i >= 0 && i < n {
			m.jumpTo(i)
		}
	case m.file != nil:
		m.panel = pMerge
		if (e.X-sideW)/max((m.w-sideW)/3, 1) == 0 {
			m.focus = Ours
		} else if (e.X-sideW)/max((m.w-sideW)/3, 1) >= 2 {
			m.focus = Theirs
		}
		if rows, r := m.rows(), m.scroll+e.Y-1; e.Y > 0 && r < len(rows) && rows[r].chunk >= 0 {
			m.cur = rows[r].chunk
		}
	}
}

// ---- layout ----

// layout gives the sidebar width and the Files panel height (with borders).
func (m model) layout() (sideW, filesH int) {
	sideW = min(max(m.w/4, 26), 40)
	if m.w < 100 {
		sideW = max(m.w/3, 16)
	}
	filesH = min(max(len(m.files)+2, 3), max((m.h-1)/2, 3))
	return sideW, filesH
}

// Merge pane rows: everything but the bottom bar and the pane borders.
func (m model) bodyHeight() int { return max(m.h-3, 1) }

// listOffset is the first visible item of a list so that sel is in view.
func listOffset(sel, n, visible int) int {
	return max(min(sel-visible+1, n-visible), 0)
}

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

// chunkLines gives the line in the resolved file where each chunk starts.
func (m model) chunkLines() []int {
	var out []int
	line := 1
	for _, s := range m.file.Segs {
		if s.Chunk == nil {
			line += len(s.Lines)
			continue
		}
		out = append(out, line)
		line += len(s.Chunk.Result())
	}
	return out
}

// ---- rendering ----

// box draws a rounded panel of w x h with the title in the top border and
// the footer in the bottom border, lazygit style.
func box(title, footer string, lines []string, w, h int, border lipgloss.TerminalColor, ts lipgloss.Style) string {
	iw, ih := max(w-2, 0), max(h-2, 0)
	bs := fg(border)
	title = runewidth.Truncate(title, max(iw-3, 0), "…")
	footer = runewidth.Truncate(footer, max(iw-3, 0), "")
	tw, fw := runewidth.StringWidth(title), runewidth.StringWidth(footer)
	var b strings.Builder
	b.WriteString(bs.Render("╭─") + ts.Render(title) + bs.Render(strings.Repeat("─", max(iw-1-tw, 0))+"╮") + "\n")
	for i := range ih {
		l := ""
		if i < len(lines) {
			l = ansi.Truncate(lines[i], iw, "")
		}
		b.WriteString(bs.Render("│") + l + "\x1b[0m" + strings.Repeat(" ", max(iw-ansi.StringWidth(l), 0)) + bs.Render("│") + "\n")
	}
	b.WriteString(bs.Render("╰"+strings.Repeat("─", max(iw-1-fw, 0))) + fg(cSub).Render(footer) + bs.Render("─╯"))
	return b.String()
}

// overlay draws top in the centre of base.
func overlay(base, top string, w, h int) string {
	bl, tl := strings.Split(base, "\n"), strings.Split(top, "\n")
	tw := lipgloss.Width(top)
	x, y := max((w-tw)/2, 0), max((h-len(tl))/2, 0)
	for i, l := range tl {
		if y+i >= len(bl) {
			break
		}
		left := ansi.Truncate(bl[y+i], x, "")
		left += strings.Repeat(" ", max(x-ansi.StringWidth(left), 0))
		bl[y+i] = left + "\x1b[0m" + l + "\x1b[0m" + ansi.TruncateLeft(bl[y+i], x+tw, "")
	}
	return strings.Join(bl, "\n")
}

func (m model) panelStyle(p panel) (lipgloss.TerminalColor, lipgloss.Style) {
	if m.panel == p {
		return cGreen, fg(cGreen).Bold(true)
	}
	return cSurface1, fg(cSub)
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
	sideW, filesH := m.layout()
	full := m.h - 1
	side := m.filesBox(sideW, filesH) + "\n" + m.conflictsBox(sideW, full-filesH)
	view := lipgloss.JoinHorizontal(lipgloss.Top, side, m.mainView(m.w-sideW, full)) + "\n" + m.bottomBar()
	if m.help {
		view = overlay(view, m.helpBox(), m.w, m.h)
	}
	if m.confirm != nil {
		view = overlay(view, m.confirmBox(), m.w, m.h)
	}
	return view
}

func (m model) filesBox(w, h int) string {
	iw, ih := w-2, h-2
	var lines []string
	if len(m.files) == 0 {
		lines = append(lines, fg(cGreen).Render(" ✓ all clear"))
	}
	for i := listOffset(m.cursor, len(m.files), ih); i < len(m.files) && len(lines) < ih; i++ {
		f := m.files[i]
		count := fmt.Sprint(f.conflicts)
		if f.conflicts < 0 {
			count = "!"
		}
		icon, iconFg := "  ", lipgloss.TerminalColor(cPeach)
		if m.file != nil && m.file.Path == f.path {
			icon = "▶ "
			if m.dirty() {
				icon = "● "
			}
		}
		name := runewidth.Truncate(f.path, max(iw-4-len(count), 1), "…")
		gap := strings.Repeat(" ", max(iw-3-runewidth.StringWidth(name)-len(count), 1))
		st := lipgloss.NewStyle()
		if i == m.cursor {
			st = st.Background(cSurface0)
			if m.panel == pFiles {
				st = st.Background(cSurface1).Bold(true)
			}
		}
		lines = append(lines, st.Foreground(iconFg).Render(icon)+st.Foreground(cText).Render(name+gap)+
			st.Foreground(cYellow).Render(count)+st.Render(" "))
	}
	footer := ""
	if len(m.files) > 0 {
		footer = fmt.Sprintf("%d of %d", m.cursor+1, len(m.files))
	}
	border, ts := m.panelStyle(pFiles)
	return box("[1] Files", footer, lines, w, h, border, ts)
}

func (m model) conflictsBox(w, h int) string {
	iw, ih := w-2, h-2
	var lines []string
	footer := ""
	if m.file != nil {
		chunks := m.file.Chunks()
		starts := m.chunkLines()
		for i := listOffset(m.cur, len(chunks), ih); i < len(chunks) && len(lines) < ih; i++ {
			c := chunks[i]
			choice := c.Choice()
			icon := "✓ "
			switch choice {
			case "pending":
				icon = "● "
			case "auto-merged":
				icon = "✦ "
			}
			left := fmt.Sprintf("#%-3d", i+1)
			at := fmt.Sprintf("L%d", starts[i])
			gap := strings.Repeat(" ", max(iw-2-len(left)-len(at)-len(choice)-2, 1))
			st := lipgloss.NewStyle()
			if i == m.cur {
				st = st.Background(cSurface0)
				if m.panel == pConflicts {
					st = st.Background(cSurface1).Bold(true)
				}
			}
			lines = append(lines, st.Foreground(choiceFg[choice]).Render(icon)+st.Foreground(cText).Render(left)+
				st.Foreground(cOverlay).Render(at+gap)+st.Foreground(choiceFg[choice]).Render(choice)+st.Render(" "))
		}
		footer = fmt.Sprintf("%d of %d", m.cur+1, len(chunks))
	}
	border, ts := m.panelStyle(pConflicts)
	return box("[2] Conflicts", footer, lines, w, h, border, ts)
}

func (m model) mainView(w, h int) string {
	if m.file == nil {
		var content string
		switch {
		case m.loadErr != "":
			content = fg(cRed).Render("✗ " + m.loadErr)
		case len(m.files) == 0:
			content = fg(cGreen).Bold(true).Render("✓ No conflicted files. All clear! 🎉")
		default:
			content = fg(cSub).Render("Select a file in [1] Files")
		}
		content = badge("◆ lazymerge", cMauve) + "\n" + fg(cSub).Render("merge conflicts, the lazy way") + "\n\n" +
			lipgloss.NewStyle().Width(max(w-6, 1)).Align(lipgloss.Center).Render(content)
		body := lipgloss.Place(w-2, h-2, lipgloss.Center, lipgloss.Center, content, lipgloss.WithWhitespaceChars(" "))
		border, ts := m.panelStyle(pMerge)
		return box("[3] Merge", "", strings.Split(body, "\n"), w, h, border, ts)
	}

	chunks := m.file.Chunks()
	done := 0
	for _, c := range chunks {
		if c.Done() {
			done++
		}
	}
	rows := m.rows()
	bodyH := h - 2
	start := min(m.scroll, max(len(rows)-1, 0))
	end := min(start+bodyH, len(rows))
	widths := [3]int{w / 3, w - 2*(w/3), w / 3}
	titles := [3]string{"[3] Ours · " + m.file.Labels[Ours], "Result", "Theirs · " + m.file.Labels[Theirs]}
	footers := [3]string{"", fmt.Sprintf("%d/%d resolved", done, len(chunks)), ""}
	if done == len(chunks) {
		footers[1] = "✓ all resolved, s to save"
	}
	var panes [3]string
	for p := range 3 {
		inner := max(widths[p]-2, 4)
		var lines []string
		for _, r := range rows[start:end] {
			lines = append(lines, m.cell(r, p, inner, chunks))
		}
		border, ts := lipgloss.TerminalColor(cSurface1), fg(paneFg[p])
		if p == m.focus*2 {
			border, ts = cOverlay, ts.Bold(true)
			if m.panel == pMerge {
				border = paneFg[p]
			}
		}
		panes[p] = box(titles[p], footers[p], lines, widths[p], h, border, ts)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panes[:]...)
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

// keyHints takes key, description pairs. Each key gets the next rainbow colour.
func keyHints(keys ...string) string {
	var parts []string
	for i := 0; i+1 < len(keys); i += 2 {
		parts = append(parts, fg(rainbow[i/2%len(rainbow)]).Bold(true).Render(keys[i])+" "+fg(cSub).Render(keys[i+1]))
	}
	return " " + strings.Join(parts, fg(cSurface1).Render(" │ "))
}

func (m model) bottomBar() string {
	var left string
	switch {
	case m.msg != "" && m.msgErr:
		left = " " + fg(cRed).Bold(true).Render("✗ "+m.msg)
	case m.msg != "":
		left = " " + fg(cGreen).Render(m.msg)
	case m.panel == pFiles:
		left = keyHints("enter", "open", "e", "edit", "R", "refresh", "?", "keybindings", "q", "quit")
	default:
		left = keyHints("space", "pick", "b", "both", "x", "decline", "z", "undo", "m", "auto", "e", "edit", "s", "save", "?", "keybindings")
	}
	right := fg(cMauve).Bold(true).Render("lazymerge ")
	if m.dirty() {
		right = fg(cPeach).Render("● unsaved  ") + right
	}
	room := m.w - lipgloss.Width(right) - 1
	if lipgloss.Width(left) > room {
		left = ansi.Truncate(left, max(room, 0), "…")
	}
	return left + strings.Repeat(" ", max(m.w-lipgloss.Width(left)-lipgloss.Width(right), 0)) + right
}

var keyHelp = []struct {
	section string
	keys    [][2]string
}{
	{"Global", [][2]string{
		{"1 2 3", "go to panel"}, {"tab / shift+tab", "next / previous panel"}, {"e", "edit file in your editor"},
		{"R", "refresh files"}, {"esc", "back to files"}, {"?", "keybindings"}, {"q", "quit"}}},
	{"Files", [][2]string{{"j k", "move (shows the file)"}, {"enter / l", "open file"}}},
	{"Conflicts and merge", [][2]string{
		{"j k", "next / previous conflict"}, {"h l", "select ours / theirs"},
		{"space", "pick selected change (again to unpick)"}, {"b", "pick both"}, {"x", "decline selected change"},
		{"z", "undo"}, {"n", "next open conflict"}, {"m", "auto-resolve simple conflicts"},
		{"y / Y", "copy change / result"}, {"ctrl+d ctrl+u", "scroll"}, {"s", "save and stage"}}},
	{"Mouse", [][2]string{{"click", "select panel, file, conflict, side"}, {"wheel", "move / scroll"}}},
}

func (m model) helpBox() string {
	var lines []string
	for _, s := range keyHelp {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+fg(cPeach).Bold(true).Render(s.section))
		for _, k := range s.keys {
			lines = append(lines, "   "+fg(cMauve).Render(fmt.Sprintf("%-16s", k[0]))+fg(cText).Render(k[1]))
		}
	}
	h := min(len(lines)+2, m.h-2)
	footer := "esc to close"
	if len(lines) > h-2 {
		footer = "j/k scroll · esc to close"
		lines = lines[min(m.helpTop, len(lines)-(h-2)):]
	}
	return box("Keybindings", footer, lines, min(m.w-4, 62), h, cMauve, fg(cMauve).Bold(true))
}

func (m model) confirmBox() string {
	w := min(m.w-4, 56)
	body := lipgloss.NewStyle().Width(w - 4).Render(m.confirm.body)
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		lines = append(lines, " "+fg(cText).Render(l))
	}
	lines = append(lines, "", " "+keyHints("enter", "confirm", "esc", "cancel"))
	return box(m.confirm.title, "", lines, w, len(lines)+2, cPeach, fg(cPeach).Bold(true))
}
