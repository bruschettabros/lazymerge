package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

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
