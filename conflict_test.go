package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const twoWay = `top
<<<<<<< HEAD
ours line
=======
theirs line
>>>>>>> feature
bottom
`

func TestParseAndAcceptOrder(t *testing.T) {
	segs, nl, err := Parse(twoWay)
	if err != nil || !nl || len(segs) != 3 {
		t.Fatalf("segs=%d nl=%v err=%v", len(segs), nl, err)
	}
	f := &File{Segs: segs, TrailingNewline: nl}
	c := f.Chunks()[0]
	if c.Labels != [2]string{"HEAD", "feature"} {
		t.Fatalf("labels %q", c.Labels)
	}
	if c.HasBase || f.Text() != "top\nbottom\n" {
		t.Fatalf("pending chunk should show (empty) base, got %q", f.Text())
	}
	c.Accept(Theirs)
	c.Accept(Ours)
	if got := f.Text(); got != "top\ntheirs line\nours line\nbottom\n" {
		t.Fatalf("got %q", got)
	}
	c.Reset()
	c.Accept(Ours)
	c.Decline(Theirs)
	if !c.Done() || f.Text() != "top\nours line\nbottom\n" {
		t.Fatalf("got %q", f.Text())
	}
}

func TestToggle(t *testing.T) {
	c := &Chunk{Base: []string{"b"}, Sides: [2][]string{{"o"}, {"t"}}}
	steps := []struct {
		do     func()
		want   []string
		done   bool
		choice string
	}{
		{func() { c.Toggle(Ours) }, []string{"o"}, true, "ours"},
		{func() { c.Toggle(Theirs) }, []string{"o", "t"}, true, "both"},
		{func() { c.Toggle(Ours) }, []string{"t"}, true, "theirs"},
		{func() { c.Decline(Theirs) }, []string{"b"}, true, "base"},
		{func() { c.Reset() }, []string{"b"}, false, "pending"},
	}
	for i, st := range steps {
		st.do()
		if !reflect.DeepEqual(c.Result(), st.want) || c.Done() != st.done || c.Choice() != st.choice {
			t.Fatalf("step %d: result %q done %v choice %s", i, c.Result(), c.Done(), c.Choice())
		}
	}
}

func TestRenderWithMarkers(t *testing.T) {
	in := "a\n<<<<<<< main\no1\n||||||| base\nb1\n=======\nt1\n>>>>>>> feat\nm\n<<<<<<< main\no2\n=======\nt2\n>>>>>>> feat\n"
	segs, nl, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	f := &File{Segs: segs, TrailingNewline: nl, Labels: [2]string{"main", "feat"}}
	f.Chunks()[1].Toggle(Theirs)
	want := "a\n<<<<<<< main\no1\n||||||| base\nb1\n=======\nt1\n>>>>>>> feat\nm\nt2\n"
	if got := f.render(true); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"<<<<<<< a\nx\n", "<<<<<<< a\nx\n<<<<<<< b\n"} {
		if _, _, err := Parse(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestAutoResolve(t *testing.T) {
	cases := []struct {
		name               string
		base, ours, theirs []string
		want               []string
	}{
		{"same both sides (whitespace)", []string{"a"}, []string{"b  c"}, []string{"b c"}, []string{"b  c"}},
		{"only theirs changed", []string{"a"}, []string{"a"}, []string{"b"}, []string{"b"}},
		{"only ours changed", []string{"a"}, []string{"b"}, []string{"a"}, []string{"b"}},
		{"different lines", []string{"a", "b"}, []string{"A", "b"}, []string{"a", "B"}, []string{"A", "B"}},
		{"real conflict", []string{"a"}, []string{"b"}, []string{"c"}, nil},
	}
	for _, tc := range cases {
		c := &Chunk{Base: tc.base, HasBase: true, Sides: [2][]string{tc.ours, tc.theirs}}
		ok := c.AutoResolve()
		if ok != (tc.want != nil) || (ok && !reflect.DeepEqual(c.Result(), tc.want)) || ok != c.Done() {
			t.Errorf("%s: ok=%v result=%q", tc.name, ok, c.Result())
		}
	}
}

func run(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	cmd.Run()
}

func TestLoadAutoResolveSave(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	write := func(s string) { os.WriteFile("f.txt", []byte(s), 0o644) }
	run(t, "git", "init", "-q", "-b", "main")
	run(t, "git", "config", "merge.conflictStyle", "merge")
	write("1\na\nb\n2\nx\ny\n")
	run(t, "git", "add", ".")
	run(t, "git", "commit", "-qm", "base")
	run(t, "git", "checkout", "-qb", "feature")
	write("1\na\nB\n2\nx\nY\n")
	run(t, "git", "commit", "-qam", "theirs")
	run(t, "git", "checkout", "-q", "main")
	write("1\nA\nb\n2\nX\ny\n")
	run(t, "git", "commit", "-qam", "ours")
	run(t, "git", "merge", "-q", "feature")

	files, err := ConflictedFiles()
	if err != nil || !reflect.DeepEqual(files, []string{"f.txt"}) {
		t.Fatalf("files=%v err=%v", files, err)
	}
	f, err := Load("f.txt")
	if err != nil {
		t.Fatal(err)
	}

	if n := len(f.Chunks()); n != 2 {
		t.Fatalf("want 2 chunks from the index, got %d", n)
	}
	for _, c := range f.Chunks() {
		if !c.HasBase || !c.AutoResolve() {
			t.Fatalf("expected auto-resolve with base, base=%q", c.Base)
		}
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile("f.txt")
	if string(data) != "1\nA\nB\n2\nX\nY\n" {
		t.Fatalf("got %q", data)
	}
	if files, _ := ConflictedFiles(); len(files) != 0 {
		t.Fatalf("still conflicted: %v", files)
	}
	if strings.Contains(string(data), "<<<<<<<") {
		t.Fatal("markers left in file")
	}
}
