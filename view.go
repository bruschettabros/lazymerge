package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

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
