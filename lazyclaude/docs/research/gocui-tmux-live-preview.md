# Live Terminal Preview in a gocui Panel: tmux capture-pane + ANSI Rendering

## Summary

Showing a live tmux pane inside a gocui `View` requires solving two independent problems: (1) controlling the captured text width so content does not overflow the panel boundary, and (2) rendering ANSI color escape codes that `capture-pane -e` produces. Both have clean solutions. The idiomatic Go approach uses `muesli/reflow/truncate` (or `charmbracelet/x/ansi`) for width-aware ANSI truncation, writes the result directly to a gocui `View` (which has a built-in escape interpreter), and drives the pane width by resizing a dedicated off-screen tmux window before capture rather than by trying to clip output after the fact.

---

## 1. tmux capture-pane — Width Control

### What the man page says

`capture-pane` flags: `[-aepPqCJMN] [-b buffer-name] [-E end-line] [-S start-line] [-t target-pane]`

There is **no `-w` width flag**. The capture reflects whatever the pane's actual terminal width is at capture time.

### The canonical trick: resize before capture

Because `capture-pane` has no width parameter, the only way to control the column count is to change the pane's dimensions first. The pattern used by tools like `fzf --preview` and various tmux scripting recipes is:

```
# Create a hidden window/session at the desired width
tmux new-session -d -s preview_session -x <WIDTH> -y <HEIGHT>
# ... or resize the target pane
tmux resize-window -t <target> -x <WIDTH>
tmux capture-pane -t <target> -ep
```

For a gocui preview panel that is W columns wide, the sequence is:

1. Before starting the gocui loop, create a dedicated tmux session at exactly W columns (or whatever the panel width will be).
2. Run the target process inside that session/window.
3. Poll with `tmux capture-pane -t <pane> -ep` (with `-e` for ANSI codes).
4. On panel resize, call `tmux resize-window -t <session>:<window> -x <new_width>` and then re-capture.

Alternatively, if the target pane already exists at an arbitrary width, capture at its native width and do the width-clipping in Go (see section 3).

### `-e` flag: raw ANSI escape codes

`capture-pane -e` includes "escape sequences for text and background attributes" in stdout. Without `-e`, output is plain text — colors are lost. With `-e`, you get raw SGR sequences (`\x1b[38;5;…m`, `\x1b[0m`, etc.) which gocui's escape interpreter can parse directly.

### `-p` flag

Writes output to stdout instead of a paste buffer. Always pair `-p` with `-e`: `tmux capture-pane -t <pane> -ep`.

---

## 2. gocui View — ANSI Rendering

### Built-in escape interpreter

`jesseduffield/gocui` (the fork lazygit vendors, not the original `jroimartin/gocui`) contains an internal `escapeInterpreter` field on every `View`. It is invoked automatically inside `parseInput` when bytes are written. It reads SGR sequences and stores `curFgColor` / `curBgColor` which are applied to each cell as it is rendered via tcell.

This means **you can write raw ANSI escape codes directly to a gocui View**:

```go
v, _ := g.View("preview")
fmt.Fprint(v, capturedANSIString)  // colors render correctly
```

No manual conversion from ANSI to gocui `Attribute` constants is needed — the interpreter handles it.

### OutputMode

Gocui delegates actual terminal output to tcell. The `OutputMode` controls color depth:

| Constant        | Colors | Notes                                      |
|-----------------|--------|--------------------------------------------|
| `OutputNormal`  | 8      | Basic terminal colors                      |
| `Output256`     | 256    | 8-bit palette                              |
| `OutputTrue`    | 24-bit | Recommended even if terminal caps are unknown; tcell degrades gracefully |

Set `OutputTrue` in `NewGuiOpts` unless you have a specific reason not to:

```go
g, _ := gocui.NewGui(gocui.OutputTrue, true)
```

### Wrap vs. truncation

| `Wrap` setting | Behavior |
|---------------|----------|
| `true`        | Lines longer than view width are word-wrapped (x-origin ignored) |
| `false`       | Lines extend beyond view width; horizontal scrolling via x-origin |

For a pane preview where the source content already fits the view width (because you pre-resized the tmux window), set `Wrap: false`. For content captured at an arbitrary width, pre-truncate in Go (see section 3) and set `Wrap: false` to avoid double-wrapping artifacts.

---

## 3. ANSI-Aware Truncation in Go

If you cannot control the captured pane width (e.g., the pane is shared with a real user session), clip each line to the view width after capture, preserving escape codes.

### Option A: `muesli/reflow/truncate`

```
go get github.com/muesli/reflow
```

```go
import "github.com/muesli/reflow/truncate"

clipped := truncate.String(line, uint(viewWidth))
// or with a tail indicator:
clipped := truncate.StringWithTail(line, uint(viewWidth), "…")
```

`truncate.String` computes printable column width by skipping escape sequences, then clips at the correct byte boundary. ANSI sequences are preserved up to the cut point. This is the library used by `charmbracelet/bubbletea` components.

### Option B: `charmbracelet/x/ansi`

```
go get github.com/charmbracelet/x/ansi
```

```go
import "github.com/charmbracelet/x/ansi"

clipped := ansi.Truncate(line, viewWidth, "")
width  := ansi.StringWidth(line) // visual width ignoring escapes
```

Also provides `Hardwrap`, `Wrap`, `Cut`, and `Strip`. More actively maintained as of 2025.

### Option C: `acarl005/stripansi` (strip only, no truncation)

Use only when you want plain text with no colors:

```go
import "github.com/acarl005/stripansi"

plain := stripansi.Strip(line)
```

---

## 4. How lazygit Does It

lazygit uses the `jesseduffield/gocui` fork with `OutputTrue`. It writes ANSI-colored diff output (from `git diff` or a configured pager like `delta`) directly to a gocui `View` using `v.SetContent(string)`. The internal escape interpreter converts SGR sequences to tcell color attributes at render time. lazygit does not truncate lines itself — it relies on the view's `Wrap: false` mode and lets users scroll horizontally, or it uses a pager configured with `{{columnWidth}}` (the panel's current column count passed as an argument to the external pager command) to produce pre-wrapped output.

The `{{columnWidth}}` template variable is substituted when lazygit invokes the pager subprocess, giving the external tool the exact column budget for the current panel.

---

## 5. Recommended Pattern for gocui Live Preview

```
Architecture:

  [tmux pane at width W] ──capture-pane -ep──► [Go goroutine]
                                                      │
                                          truncate each line to W
                                          (muesli/reflow or charmbracelet/x/ansi)
                                                      │
                                            v.Clear(); fmt.Fprint(v, content)
                                                      │
                                          [gocui View, OutputTrue, Wrap:false]
```

### Step-by-step

1. At gocui startup, read the preview panel's width: `v.Width()`.
2. If you control the tmux window: `tmux resize-window -t <win> -x <width>` so lines fit natively.
3. On a ticker (e.g., 200 ms) or event: run `tmux capture-pane -t <pane> -ep` and collect stdout.
4. For each line, apply `ansi.Truncate(line, panelWidth, "")` as a safety clamp.
5. Write the result to the gocui view inside `g.Update(func(g *gocui.Gui) error { ... })` to stay on the UI goroutine.
6. On terminal resize (`gocui.ResizeHandler`): re-read panel width, re-resize the tmux window, re-capture.

### What NOT to do

- Do not use `capture-pane` without `-e` if you need colors. The plain-text output permanently loses color information.
- Do not set `Wrap: true` on a view that receives pre-formatted terminal content — tmux already handles line endings; double-wrapping breaks the layout.
- Do not use `tmux pipe-pane` for a live preview polling loop. `pipe-pane` is a persistent pipe for logging, not for snapshot reads. Use `capture-pane` in a polling goroutine instead.
- Do not try to use the original `jroimartin/gocui` for this — it does not have the escape interpreter. Use `jesseduffield/gocui` or `awesome-gocui/gocui`.

---

## Key References

- tmux man page — capture-pane flags: [man7.org/linux/man-pages/man1/tmux.1.html](https://man7.org/linux/man-pages/man1/tmux.1.html)
- jesseduffield/gocui View source (escape interpreter, OutputMode): [github.com/jesseduffield/gocui](https://github.com/jesseduffield/gocui)
- muesli/reflow truncate package: [pkg.go.dev/github.com/muesli/reflow/truncate](https://pkg.go.dev/github.com/muesli/reflow/truncate)
- charmbracelet/x/ansi (Truncate, StringWidth, Hardwrap): [pkg.go.dev/github.com/charmbracelet/x/ansi](https://pkg.go.dev/github.com/charmbracelet/x/ansi)
- acarl005/stripansi (strip only): [github.com/acarl005/stripansi](https://github.com/acarl005/stripansi)
- lazygit custom pagers ({{columnWidth}} template): [github.com/jesseduffield/lazygit/blob/master/docs/Custom_Pagers.md](https://github.com/jesseduffield/lazygit/blob/master/docs/Custom_Pagers.md)
- muesli/termenv (color profile detection): [github.com/muesli/termenv](https://github.com/muesli/termenv)
