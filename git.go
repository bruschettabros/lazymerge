package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrNoConflicts = errors.New("no conflict blocks in file (binary, deleted, or already resolved?)")

func git(args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func ConflictedFiles() ([]string, error) {
	out, err := git("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	segs, nl, err := Parse(string(data))
	if err != nil {
		return nil, err
	}
	f := &File{Path: path, Segs: segs, TrailingNewline: nl}
	if len(f.Chunks()) == 0 {
		return nil, ErrNoConflicts
	}
	f.Labels = f.Chunks()[0].Labels
	if f.Labels[Ours] == "HEAD" {
		if b, err := git("rev-parse", "--abbrev-ref", "HEAD"); err == nil && strings.TrimSpace(b) != "HEAD" {
			f.Labels[Ours] = strings.TrimSpace(b)
		}
	}
	addBaseFromStages(f)
	return f, nil
}

func addBaseFromStages(f *File) {
	chunks := f.Chunks()
	if chunks[0].HasBase {
		return
	}
	dir, err := os.MkdirTemp("", "lazymerge")
	if err != nil {
		return
	}
	defer os.RemoveAll(dir)
	var names [3]string
	for i, stage := range []string{"2", "1", "3"} {
		blob, err := git("show", ":"+stage+":"+f.Path)
		if err != nil {
			return
		}
		names[i] = filepath.Join(dir, stage)
		if os.WriteFile(names[i], []byte(blob), 0o600) != nil {
			return
		}
	}

	out, _ := exec.Command("git", "merge-file", "-p", "--diff3", names[0], names[1], names[2]).Output()
	segs, _, err := Parse(string(out))
	if err != nil || len(segs) == 0 {
		return
	}

	alt := &File{Segs: segs}
	if len(alt.Chunks()) > 0 && alt.view(Ours) == f.view(Ours) && alt.view(Theirs) == f.view(Theirs) {
		f.Segs = segs
	}
}

func (f *File) Save() error {
	for _, c := range f.Chunks() {
		if !c.Done() {
			return errors.New("not all conflicts are resolved")
		}
	}
	info, err := os.Stat(f.Path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(f.Path, []byte(f.Text()), info.Mode().Perm()); err != nil {
		return err
	}
	_, err = git("add", "--", f.Path)
	return err
}

func (f *File) WriteProgress() error {
	info, err := os.Stat(f.Path)
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, []byte(f.render(true)), info.Mode().Perm())
}
