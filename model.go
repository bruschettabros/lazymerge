package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

type panel int

const (
	pFiles panel = iota
	pConflicts
	pMerge
)

type fileItem struct {
	path      string
	conflicts int
}

type confirm struct {
	title, body string
	yes         func(m *model) tea.Cmd
}

type model struct {
	files   []fileItem
	cursor  int
	file    *File
	loadErr string
	lexer   chroma.Lexer
	theme   *chroma.Style
	panel   panel
	cur     int
	focus   int
	scroll  int
	w, h    int
	msg     string
	msgErr  bool
	undo    [][]Chunk
	help    bool
	helpTop int
	confirm *confirm
}

type row struct {
	cells [3]string
	chunk int
	first bool
	empty [3]bool
}

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
		n := -1
		if f, err := Load(p); err == nil {
			n = len(f.Chunks())
		}
		m.files = append(m.files, fileItem{p, n})
	}
	m.cursor = min(m.cursor, max(len(m.files)-1, 0))
}

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

func (m model) layout() (sideW, filesH int) {
	sideW = min(max(m.w/4, 26), 40)
	if m.w < 100 {
		sideW = max(m.w/3, 16)
	}
	filesH = min(max(len(m.files)+2, 3), max((m.h-1)/2, 3))
	return sideW, filesH
}

func (m model) bodyHeight() int { return max(m.h-3, 1) }

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
