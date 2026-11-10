package main

import (
	"errors"
	"fmt"
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

type Chunk struct {
	Sides   [2][]string
	Labels  [2]string
	Base    []string
	HasBase bool
	States  [2]State
	applied []int
	merged  []string
}

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

func (f *File) Text() string { return f.render(false) }

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
