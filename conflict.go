package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type State int

const (
	Pending State = iota
	Applied
	Ignored
)

const (
	Ours   = 0
	Theirs = 1
)

// Chunk is one conflict. Sides[Ours] / Sides[Theirs] are the two changes.
type Chunk struct {
	Sides   [2][]string
	Labels  [2]string // marker labels, e.g. "HEAD" and "feature"
	Base    []string
	HasBase bool
	States  [2]State
	applied []int    // sides in the order they were accepted
	merged  []string // set by line-level auto-merge
}

// Segment is either plain text (Chunk == nil) or a conflict.
type Segment struct {
	Lines []string
	Chunk *Chunk
}

type File struct {
	Path            string
	Labels          [2]string
	Segs            []Segment
	TrailingNewline bool
}

func (c *Chunk) Accept(side int) {
	if c.States[side] != Pending || c.merged != nil {
		return
	}
	c.States[side] = Applied
	c.applied = append(c.applied, side)
}

// Toggle is the space key: accept a side (declining the other side if it is
// still open), or un-accept it if it is already accepted. Accepting the
// second side appends it, which gives "both".
func (c *Chunk) Toggle(side int) {
	if c.merged != nil {
		c.Reset()
	}
	if c.States[side] == Applied {
		c.Decline(side)
		return
	}
	c.States[side] = Applied
	c.applied = append(c.applied, side)
	if c.States[1-side] == Pending {
		c.States[1-side] = Ignored
	}
}

func (c *Chunk) Decline(side int) {
	if c.merged != nil {
		c.Reset()
	}
	c.States[side] = Ignored
	c.applied = slices.DeleteFunc(c.applied, func(s int) bool { return s == side })
}

func (c *Chunk) Reset() {
	c.States = [2]State{}
	c.applied, c.merged = nil, nil
}

func (c *Chunk) Done() bool { return c.States[Ours] != Pending && c.States[Theirs] != Pending }

func (c *Chunk) AutoMerged() bool { return c.merged != nil }

// Choice names what the chunk resolves to, for the conflicts list.
func (c *Chunk) Choice() string {
	switch {
	case c.merged != nil:
		return "auto-merged"
	case !c.Done():
		return "pending"
	case len(c.applied) == 2:
		return "both"
	case len(c.applied) == 0:
		return "base"
	case c.applied[0] == Ours:
		return "ours"
	}
	return "theirs"
}

// Result is what goes into the file for this chunk. Like JetBrains, the base
// text stays until a side is accepted.
func (c *Chunk) Result() []string {
	if c.merged != nil {
		return c.merged
	}
	if len(c.applied) == 0 {
		return c.Base
	}
	var out []string
	for _, s := range c.applied {
		out = append(out, c.Sides[s]...)
	}
	return out
}

// AutoResolve resolves the chunk when the answer is unambiguous. It reports
// whether it changed the chunk.
func (c *Chunk) AutoResolve() bool {
	if c.Done() {
		return false
	}
	ours, theirs := c.Sides[Ours], c.Sides[Theirs]
	switch {
	case equal(ours, theirs, true):
		c.Reset()
		c.Accept(Ours)
		c.Decline(Theirs)
	case c.HasBase && equal(c.Base, ours, false):
		c.Reset()
		c.Decline(Ours)
		c.Accept(Theirs)
	case c.HasBase && equal(c.Base, theirs, false):
		c.Reset()
		c.Accept(Ours)
		c.Decline(Theirs)
	default:
		m := lineMerge(c.Base, ours, theirs, c.HasBase)
		if m == nil {
			return false
		}
		c.Reset()
		c.merged = m
		c.States = [2]State{Applied, Applied}
	}
	return true
}

// lineMerge handles changes to different lines of a same-length block,
// e.g. base [a b], ours [A b], theirs [a B] -> [A B].
func lineMerge(base, ours, theirs []string, hasBase bool) []string {
	if !hasBase || len(base) != len(ours) || len(base) != len(theirs) {
		return nil
	}
	out := make([]string, len(base))
	for i := range base {
		switch {
		case ours[i] == base[i]:
			out[i] = theirs[i]
		case theirs[i] == base[i], ours[i] == theirs[i]:
			out[i] = ours[i]
		default:
			return nil
		}
	}
	return out
}

func equal(a, b []string, ignoreSpace bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if ignoreSpace {
			x, y = strings.Join(strings.Fields(x), " "), strings.Join(strings.Fields(y), " ")
		}
		if x != y {
			return false
		}
	}
	return true
}

func isMarker(line, m string) bool {
	return strings.HasPrefix(line, m) && (len(line) == len(m) || line[len(m)] == ' ')
}

// Parse splits text with git conflict markers (merge, diff3 or zdiff3 style)
// into segments.
func Parse(text string) (segs []Segment, trailingNewline bool, err error) {
	trailingNewline = strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	const (
		plain = iota
		inOurs
		inBase
		inTheirs
	)
	state := plain
	var cur []string
	var c *Chunk
	flushPlain := func() {
		if len(cur) > 0 {
			segs = append(segs, Segment{Lines: cur})
		}
		cur = nil
	}
	for i, l := range lines {
		switch {
		case state == plain && isMarker(l, "<<<<<<<"):
			flushPlain()
			c = &Chunk{Labels: [2]string{strings.TrimSpace(l[7:])}}
			state = inOurs
		case state == inOurs && isMarker(l, "|||||||"):
			c.Sides[Ours], cur = cur, nil
			c.HasBase = true
			state = inBase
		case (state == inOurs || state == inBase) && l == "=======":
			if state == inOurs {
				c.Sides[Ours] = cur
			} else {
				c.Base = cur
			}
			cur = nil
			state = inTheirs
		case state == inTheirs && isMarker(l, ">>>>>>>"):
			c.Sides[Theirs], cur = cur, nil
			c.Labels[Theirs] = strings.TrimSpace(l[7:])
			segs = append(segs, Segment{Chunk: c})
			state = plain
		case state != plain && (isMarker(l, "<<<<<<<") || isMarker(l, ">>>>>>>")):
			return nil, false, fmt.Errorf("line %d: unexpected conflict marker", i+1)
		default:
			cur = append(cur, l)
		}
	}
	if state != plain {
		return nil, false, errors.New("unterminated conflict block")
	}
	flushPlain()
	return segs, trailingNewline, nil
}

func (f *File) Chunks() []*Chunk {
	var out []*Chunk
	for _, s := range f.Segs {
		if s.Chunk != nil {
			out = append(out, s.Chunk)
		}
	}
	return out
}

// Text is the resolved file content.
func (f *File) Text() string { return f.render(false) }

// render builds the file text. With markers set, open conflicts are written
// back as git conflict markers (with the base, when known).
func (f *File) render(markers bool) string {
	var lines []string
	for _, s := range f.Segs {
		c := s.Chunk
		switch {
		case c != nil && markers && !c.Done():
			lines = append(lines, strings.TrimSpace("<<<<<<< "+f.Labels[Ours]))
			lines = append(lines, c.Sides[Ours]...)
			if c.HasBase {
				lines = append(lines, "||||||| base")
				lines = append(lines, c.Base...)
			}
			lines = append(lines, "=======")
			lines = append(lines, c.Sides[Theirs]...)
			lines = append(lines, strings.TrimSpace(">>>>>>> "+f.Labels[Theirs]))
		case c != nil:
			lines = append(lines, c.Result()...)
		default:
			lines = append(lines, s.Lines...)
		}
	}
	out := strings.Join(lines, "\n")
	if f.TrailingNewline && len(lines) > 0 {
		out += "\n"
	}
	return out
}

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

// ConflictedFiles lists unmerged paths, relative to the repo root.
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

// addBaseFromStages gets the base text (and finer chunks) from the index
// stages when the file uses the plain 2-way marker style. It only does this
// when the result holds the same ours and theirs text, so user edits stay.
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
	for i, stage := range []string{"2", "1", "3"} { // merge-file order: ours base theirs
		blob, err := git("show", ":"+stage+":"+f.Path)
		if err != nil {
			return
		}
		names[i] = filepath.Join(dir, stage)
		if os.WriteFile(names[i], []byte(blob), 0o600) != nil {
			return
		}
	}
	// merge-file exits with the number of conflicts, so ignore the exit code.
	out, _ := exec.Command("git", "merge-file", "-p", "--diff3", names[0], names[1], names[2]).Output()
	segs, _, err := Parse(string(out))
	if err != nil || len(segs) == 0 {
		return
	}
	// Same ours view and theirs view means no user edits; only the chunk
	// borders differ (2-way style joins nearby conflicts into one).
	alt := &File{Segs: segs}
	if len(alt.Chunks()) > 0 && alt.view(Ours) == f.view(Ours) && alt.view(Theirs) == f.view(Theirs) {
		f.Segs = segs
	}
}

// view is the file text with every conflict taken from one side.
func (f *File) view(side int) string {
	var b strings.Builder
	for _, s := range f.Segs {
		lines := s.Lines
		if s.Chunk != nil {
			lines = s.Chunk.Sides[side]
		}
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// Save writes the resolved file and marks it resolved with git add.
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

// WriteProgress writes resolved conflicts and keeps open ones as markers,
// so the file can be finished in an editor. It does not stage the file.
func (f *File) WriteProgress() error {
	info, err := os.Stat(f.Path)
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, []byte(f.render(true)), info.Mode().Perm())
}
