package main

import (
	"errors"
	"os/exec"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type editedMsg struct {
	path string
	err  error
}

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
		return m, nil
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
