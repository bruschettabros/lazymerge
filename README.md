# lazymerge

A terminal UI for git merge conflicts, in the style of lazygit, lazydocker and lazysql.

The merge view works like the JetBrains merge dialog: **Ours**, **Result** and **Theirs** side
by side. You never see `<<<<<<<`, `=======` or `>>>>>>>` markers. You pick, combine or decline
each change with vim-style keys or the mouse. Then you save, and lazymerge stages the file.

```
╭─[1] Files────────────╮╭─[3] Ours · main──────────╮╭─Result───────────────────╮╭─Theirs · feature─────────╮
│▶ a.go              2 ││  func greet() {          ││  func greet() {          ││  func greet() {          │
│  shop.py           2 ││✗     fmt.Println("main") ││▎     fmt.Println("feat") ││✓     fmt.Println("feat") │
╰───────────────1 of 2─╯│  }                       ││  }                       ││  }                       │
╭─[2] Conflicts────────╮│                          ││                          ││                          │
│✓ #1  L2       theirs ││▶ const x = 9             ││▎ const x = 1             ││● const x = 1             │
│● #2  L5      pending ││▎ const y = 2             ││▎ const y = 2             ││▎ const y = 3             │
╰───────────────2 of 2─╯╰──────────────────────────╯╰────────────1/2 resolved─╯╰──────────────────────────╯
 space pick │ b both │ x decline │ z undo │ m auto │ e edit │ s save │ ? keybindings          lazymerge
```

## Install

Requires Go 1.27.3 or later and `git` on your `PATH`.

```sh
go install github.com/shaunc/lazymerge@latest
# or, from this folder:
make install                       # builds and copies to /usr/local/bin
make install PREFIX=$HOME/.local   # or to ~/.local/bin
```

## Use

1. Start a merge, rebase or cherry-pick that has conflicts.
2. Run `lazymerge` anywhere in the repository.
3. Move through `[1] Files` with `j`/`k`. The merge view shows the selected file.
4. Press `enter` to work in the merge view.
5. Press `m` to auto-resolve the simple conflicts.
6. Resolve the remaining conflicts: select a side with `h`/`l` and press `space`.
7. Press `s` to save. lazymerge writes the file, runs `git add` on it and shows the next file.
8. Finish the merge with git as usual (for example `git commit` or `git rebase --continue`).

## Layout

| Panel | Contents |
|-------|----------|
| `[1] Files` | Files with conflicts and the number of conflicts in each. `▶` is the open file, `●` means it has unsaved changes. |
| `[2] Conflicts` | The conflicts in the open file: the line, and the choice (`pending`, `ours`, `theirs`, `both`, `base`, `auto-merged`). |
| `[3] Merge` | Ours, Result and Theirs. The Result footer shows how many conflicts are resolved. |

The active panel has a green border. The bottom bar shows the main keys for the active panel,
messages, and `● unsaved` when you have unsaved changes. Press `?` to see all keys.

## Keys

### Global

| Key | Action |
|-----|--------|
| `1` `2` `3` | Go to a panel |
| `tab` / `shift+tab` | Next / previous panel |
| `e` | Edit the file in your editor (see below) |
| `R` | Refresh the file list |
| `esc` | Back to `[1] Files` |
| `?` | Show all keybindings |
| `q` | Quit (asks first if you have unsaved changes) |

### Files

| Key | Action |
|-----|--------|
| `j` `k` / `↓` `↑`, `g` `G` | Move. The merge view shows the selected file. |
| `enter` / `l` / `space` | Open the file in the merge view |

If the open file has unsaved changes, moving does not replace it, and `enter` asks before it
discards them.

### Conflicts and merge

| Key | Action |
|-----|--------|
| `j` `k` / `↓` `↑`, `g` `G` | Next / previous / first / last conflict |
| `h` `l` / `←` `→` | Select the ours (left) or theirs (right) change |
| `space` | Pick the selected change. The other side is declined if it is still pending. Press `space` on a picked change to unpick it. |
| `b` | Pick both (ours first, then theirs) |
| `x` | Decline the selected change |
| `z` | Undo (as many steps as you like) |
| `n` | Go to the next pending conflict |
| `m` | Auto-resolve all simple conflicts |
| `y` / `Y` | Copy the selected change / the current result as plain text |
| `ctrl+d` `ctrl+u`, `ctrl+e` `ctrl+y` | Scroll half a page / one line |
| `s` | Save and stage (only when all conflicts are resolved) |

To pick both changes in the other order, pick theirs first with `space`, then move to ours and
press `space` again.

### Mouse

- Click a panel to make it active. Click a file or a conflict to select it.
- Click the Ours or Theirs pane to select that side.
- Use the wheel to move through lists or to scroll the merge view.

To select text in the terminal while the mouse is on, hold `shift` (or `option` in some macOS
terminals).

## Edit in your editor

Press `e` to open the file in your editor. lazymerge uses the same editor as git:
`GIT_EDITOR`, then `core.editor`, then `$VISUAL`, then `$EDITOR`, then `vi`.

- If the file has unsaved changes, lazymerge first writes the choices you made. Conflicts that
  are still open stay in the file as normal git conflict markers, with the base text when it
  is known. It does not stage the file.
- When you close the editor, lazymerge loads the file again.
- When no conflict markers remain, lazymerge runs `git add` on the file.

GUI editors must wait until you close the file, for example
`git config --global core.editor "code --wait"`.

## Colours

lazymerge uses the [Catppuccin](https://catppuccin.com) colours: Mocha on dark terminals and
Latte on light terminals. Syntax highlighting uses the matching Catppuccin theme
([chroma](https://github.com/alecthomas/chroma)), chosen from the file name.

- Ours is blue, the result is mauve and theirs is green. Pending changes have a tinted background.
- The selected change has a brighter background and a peach `▶`.
- `●` (yellow) is pending, `✓` (green) is picked and `✗` (red, struck through) is declined.

## Auto-resolve

`m` resolves a conflict only when the answer is clear:

- Both sides made the same change (whitespace differences are ignored).
- Only one side changed the code compared with the base.
- Both sides changed different lines of the same block (line-by-line merge).

Other conflicts stay open for you. Press `z` to undo an auto-resolve.

### Where the base comes from

The default git conflict style does not include the base (the common ancestor), and it joins
nearby conflicts into one big block. lazymerge runs `git merge-file --diff3` on the index
stages to get the base and smaller blocks. It uses that result only when it contains the same
"ours" and "theirs" text as the file on disk. If you already edited the file, lazymerge uses
the markers in the file as they are.

You can also set `git config --global merge.conflictStyle zdiff3`. Then git writes the base
into the file directly.

## Limits

- Text files with conflict markers only. Binary files and delete/modify conflicts are not
  supported. Resolve those with `git checkout --ours/--theirs` or `git rm`.
- No text editing inside lazymerge. Press `e` to finish a file in your editor.
- No config file. Keys and colours are fixed.
- Each line is highlighted on its own, so multi-line strings and comments can have the
  wrong colour.
- Copy uses the system clipboard (`pbcopy` on macOS, `xclip`/`xsel`/`wl-copy` on Linux).

## Develop

| Command | Action |
|---------|--------|
| `make` | Format, vet, test and build (`make all`) |
| `make build` | Build the `./lazymerge` binary |
| `make run` | Build and start lazymerge in the current folder |
| `make test` | Run the tests |
| `make vet` | Run `go vet` |
| `make fmt` | Format the code with `gofmt` |
| `make tidy` | Run `go mod tidy` |
| `make install` / `make uninstall` | Copy to / remove from `$(PREFIX)/bin` (default `/usr/local`) |
| `make clean` | Remove the built binary |

The tests include an end-to-end test that makes a real git merge conflict in a temp folder.

### Code layout

All code is in one `main` package, with one file for each job:

| File | Contents |
|------|----------|
| `main.go` | Entry point: finds the repo root and starts the UI |
| `conflict.go` | Conflict model: parse markers, pick/decline/undo state, auto-resolve, render the file |
| `git.go` | git calls: list conflicted files, load (with the base from the index), save and stage |
| `model.go` | UI state and actions: files, selection, undo stack, layout sizes |
| `keys.go` | Keyboard and mouse input |
| `view.go` | Drawing: panels, merge panes, popups, bottom bar |
| `editor.go` | Open the file in the user's editor and reload it after |
| `theme.go` | Catppuccin colours |
