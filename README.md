# conflux

A terminal merge-conflict resolver for git, in the style of the JetBrains merge dialog.

You see three columns: **Ours** (current branch), **Result**, and **Theirs** (incoming).
You never see `<<<<<<<`, `=======` or `>>>>>>>` markers. Each conflict is a block that you
accept, combine, or decline with vim-style keys. Code has syntax highlighting. Then you save, and conflux stages the file.

```
  Ours (current branch)        │ Result                       │ Theirs (incoming)
  func greet() {               │ func greet() {               │ func greet() {
▶     fmt.Println("from main") │     fmt.Println("hi")        │     fmt.Println("from feature")
  }                            │ }                            │ }
┃ const x = 9                  │ const x = 1                  │ const x = 1
┃ const y = 2                  │ const y = 2                  │ const y = 3
```

## Install

Requires Go 1.27.3 or later and `git` on your `PATH`.

```sh
go install github.com/shaunc/conflux@latest
# or, from this folder:
make install                 # builds and copies to /usr/local/bin
make install PREFIX=$HOME/.local   # or to ~/.local/bin
```

## Use

1. Start a merge, rebase or cherry-pick that has conflicts.
2. Run `conflux` anywhere in the repository.
3. Select a file and press `enter`.
4. Press `m` to auto-resolve the simple conflicts.
5. Resolve the remaining conflicts with the keys below.
6. Press `s` to save. conflux writes the file and runs `git add` on it.
7. Finish the merge with git as usual (for example `git commit` or `git rebase --continue`).

### Keys — file list

| Key | Action |
|-----|--------|
| `j` `k` / `↓` `↑`, `g` `G` | Move, go to first / last |
| `enter` / `l` | Open the file |
| `r` | Refresh the list |
| `q` / `esc` | Quit |

### Keys — merge view

The keys work like vim. Select a change with `j`/`k` (which conflict) and `h`/`l` (which
side). Then press `space` to accept it.

| Key | Action |
|-----|--------|
| `j` `k` / `↓` `↑` | Next / previous conflict |
| `h` `l` / `←` `→` | Select the ours (left) or theirs (right) change |
| `space` | Accept the selected change. The other side is declined if it is still pending. Press `space` on an accepted change to un-accept it. |
| `x` | Decline the selected change |
| `u` | Reset the current conflict to pending |
| `n` | Go to the next pending conflict |
| `g` / `G` | First / last conflict |
| `ctrl+d` `ctrl+u`, `ctrl+e` `ctrl+y` | Scroll half a page / one line |
| `m` | Auto-resolve all simple conflicts |
| `y` / `Y` | Copy the selected change / the current result as plain text |
| `s` | Save and stage (only when all conflicts are resolved) |
| `esc` / `q` | Back to the file list (press twice to discard unsaved changes) |

To take **both** changes: accept one side with `space`, move to the other side, and press
`space` again. The result has them in the order you accepted them.

### Display

- Pending changes have a blue (ours), green (theirs) or purple (result) background.
- The selected change has a brighter background and a `▶` marker. The selected side has an
  orange border.
- `✓` is an accepted change and `✗` (struck through) is a declined change.
- The header shows a progress bar of resolved conflicts. `●` means you have unsaved changes.
- Code has syntax highlighting, chosen from the file name ([chroma](https://github.com/alecthomas/chroma)).
  The theme is `monokai` on dark terminals and `github` on light terminals.

## Auto-resolve

`m` resolves a conflict only when the answer is clear:

- Both sides made the same change (whitespace differences are ignored).
- Only one side changed the code compared with the base.
- Both sides changed different lines of the same block (line-by-line merge).

Other conflicts stay open for you.

### Where the base comes from

The default git conflict style does not include the base (the common ancestor), and it joins
nearby conflicts into one big block. conflux runs `git merge-file --diff3` on the index
stages to get the base and smaller blocks. It uses that result only when it contains the same
"ours" and "theirs" text as the file on disk. If you already edited the file, conflux uses
the markers in the file as they are.

You can also set `git config --global merge.conflictStyle zdiff3`. Then git writes the base
into the file directly.

## Limits

- Text files with conflict markers only. Binary files and delete/modify conflicts are not
  supported. Resolve those with `git checkout --ours/--theirs` or `git rm`.
- No in-place text editing of the result. Copy a side with `c`/`C`, then edit the saved
  file in your editor if you need a mix.
- Each line is highlighted on its own, so multi-line strings and comments can have the
  wrong colour.
- Copy uses the system clipboard (`pbcopy` on macOS, `xclip`/`xsel`/`wl-copy` on Linux).

## Develop

| Command | Action |
|---------|--------|
| `make` | Format, vet, test and build (`make all`) |
| `make build` | Build the `./conflux` binary |
| `make run` | Build and start conflux in the current folder |
| `make test` | Run the tests |
| `make vet` | Run `go vet` |
| `make fmt` | Format the code with `gofmt` |
| `make tidy` | Run `go mod tidy` |
| `make install` / `make uninstall` | Copy to / remove from `$(PREFIX)/bin` (default `/usr/local`) |
| `make clean` | Remove the built binary |

The tests include an end-to-end test that makes a real git merge conflict in a temp folder.
