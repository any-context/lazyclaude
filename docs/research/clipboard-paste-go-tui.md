# Clipboard Paste Handling in Go TUI Applications

## Summary

Terminal applications written in Go face a layered paste problem: the terminal protocol
defines bracketed paste mode (ESC[200~ ... ESC[201~), the terminal multiplexer (tmux)
may or may not propagate those sequences faithfully, and the TUI library (gocui/tcell)
may or may not parse them before they reach application code. The robust approach is to
implement two complementary paths: a native library path that works when tcell/gocui
correctly fires paste events, and a fallback pattern-matching path that reconstructs
paste boundaries from raw key events when the library path fails.

---

## 1. Bracketed Paste Protocol

### How it works

Bracketed paste is an xterm extension now supported by virtually all modern terminal
emulators. When active, the terminal wraps pasted text with escape sequences so the
application can distinguish a paste from manual typing:

```
\e[?2004h     -- enable bracketed paste mode (sent by the application at startup)

\e[200~       -- paste START marker (sent by the terminal before pasted text)
<paste text>
\e[201~       -- paste END marker (sent by the terminal after pasted text)

\e[?2004l     -- disable bracketed paste mode (sent by the application on exit)
```

Without bracketed paste, pasting a newline is indistinguishable from pressing Enter,
and pasting multi-line content triggers unintended command execution. With it, the
application accumulates the entire paste block before acting on it.

### Terminal support

Supported: xterm, iTerm2, Terminal.app (macOS), Ghostty, kitty, Alacritty, WezTerm,
VSCode integrated terminal, tmux (as a passthrough when bracketed paste is configured).

Not supported: Linux virtual console, older Windows cmd.exe/conhost.

### tmux and bracketed paste: the propagation problem

tmux mediates all terminal I/O for its panes. Whether ESC[200~/ESC[201~ sequences reach
the inner pane is controlled by the `set-clipboard` and `allow-passthrough` settings.
When a TUI runs inside a `display-popup`, tmux v3.4 introduced a known bug (issue #4431)
where it fails to synthesize proper bracketed-paste events, causing tcell to receive the
raw escape bytes as individual key events instead of a parsed `EventPaste`. This is the
primary failure mode for lazyclaude and similar popup-hosted TUIs.

Reference: [tmux issue #280](https://github.com/tmux/tmux/issues/280) and the Claude
Code paste corruption report at anthropics/claude-code#3134.

---

## 2. tcell: the canonical Go terminal library

tcell (github.com/gdamore/tcell/v2) is the foundation for most modern Go TUIs including
gocui, tview, and bubbletea.

### EventPaste API

```go
// Application opt-in:
screen.EnablePaste()

// In the event loop:
switch ev := screen.PollEvent().(type) {
case *tcell.EventPaste:
    if ev.Start() {
        // accumulate
    }
    if ev.End() {
        // flush accumulated text
    }
case *tcell.EventKey:
    if app.IsPasting {
        // buffer this character
    }
}
```

`EnablePaste()` writes `\e[?2004h` to the terminal. tcell parses the ESC[200~ /
ESC[201~ sequences and emits `EventPaste{start=true}` / `EventPaste{start=false}`.
Between those events, `EventKey` events carry the individual pasted characters.

Key limitation: tcell's event channel is 256 slots. If the paste is large enough to
overflow the channel, `EventPaste{End}` can be blocked indefinitely, deadlocking the
event loop. See Section 5 (watchdog pattern) for the mitigation.

Sources:
- [gdamore/tcell paste.go](https://github.com/gdamore/tcell/blob/main/paste.go)
- [tcell issue #120](https://github.com/gdamore/tcell/issues/120)

---

## 3. gocui: paste integration

### awesome-gocui and jesseduffield/gocui

The lazygit fork (jesseduffield/gocui) added bracketed paste support in late 2024 via
tcell's `EventPaste`. The integration exposes:

```go
// On the Gui struct:
g.IsPasting bool   // true while a bracketed paste is in progress

// In the custom Editor:
type Editor interface {
    Edit(v *View, key Key, ch rune, mod Modifier) bool
}
```

When tcell fires `EventPaste{start=true}`, gocui sets `g.IsPasting = true`. The
application's `Editor.Edit` method is called for every key event; when `g.IsPasting`
is true the editor knows the current key is part of a paste and can buffer it.

When tcell fires `EventPaste{start=false}`, gocui sets `g.IsPasting = false`, and the
editor flushes the buffer.

Reference: [lazygit PR #4234](https://github.com/jesseduffield/lazygit/pull/4234) —
"Fix pasting multi-line text into commit message panel".

### What lazygit does with paste

lazygit uses `IsPasting` to change the semantic of newline during paste:
- Normal typing: Enter confirms the commit message.
- During paste: Enter is treated as a line separator (moves to the description field
  rather than submitting).

The implementation in lazygit's vendor copy of gocui routes `*tcell.EventPaste` from
`tcell_driver.go` into a `GocuiEvent{Type: eventPaste, Start: bool}` and from there
into the `g.IsPasting` flag.

---

## 4. Fallback: raw ESC sequence pattern matching

When tcell fails to parse the bracketed paste sequences (e.g., tmux display-popup bug),
the bytes arrive as individual gocui key events in this order:

```
KeyEsc, '[', '2', '0', '0', '~'   -- paste start
<content characters>
KeyEsc, '[', '2', '0', '1', '~'   -- paste end
```

The fallback approach buffers events that start with Esc and pattern-matches against
the known suffixes:

```go
const pasteStartSuffix = "[200~"
const pasteEndSuffix   = "[201~"

func (e *editor) Edit(v *View, key Key, ch rune, mod Modifier) bool {
    // Fast path: tcell already parsed the paste event.
    if e.gui.IsPasting && !e.inPaste {
        e.inPaste = true
        e.notifyWatchdog()
    }
    if e.inPaste {
        return e.handlePaste(key, ch, mod)
    }

    // Slow path: detect raw ESC sequence.
    if key == KeyEsc {
        e.escBuf = append(e.escBuf[:0], editEvent{key, ch, mod})
        e.startEscTimer() // flush standalone Esc after ~10ms
        return true
    }
    if len(e.escBuf) > 0 {
        return e.handleEscSeq(key, ch, mod)
    }
    return e.forwardAny(key, ch, mod)
}
```

The Esc timer (10 ms) distinguishes a standalone Escape keypress from the start of
`ESC[200~`. A paste start sequence arrives in a burst within a single event-loop cycle;
a human Esc press will not be followed by `[200~` within 10 ms.

---

## 5. Watchdog goroutine for large pastes

tcell's event channel has 256 slots. A paste of more than ~250 characters can fill it
before `EventPaste{End}` is delivered, blocking the event loop goroutine and deadlocking
the entire TUI.

The watchdog pattern:

```go
func (a *App) startPasteWatchdog() {
    ch := a.editor.pasteNotify  // buffered channel, cap 1
    go func() {
        for range ch {
            for {
                // Poll: if paste is ongoing AND buffer has data, drain it.
                time.Sleep(200 * time.Millisecond)
                a.editor.pasteMu.Lock()
                stillPasting := a.editor.inPaste
                hasData := a.editor.pasteBuf.Len() > 0
                a.editor.pasteMu.Unlock()
                if !stillPasting {
                    break
                }
                if hasData {
                    a.editor.drainPaste()  // flush partial buffer, keep inPaste=true
                }
            }
        }
    }()
}
```

`drainPaste` flushes accumulated content to the target pane without clearing `inPaste`,
so subsequent characters continue buffering. `flushPaste` (called when the end marker
arrives) is the terminal flush that also resets `inPaste=false`.

---

## 6. tmux paste-buffer: the delivery mechanism

For a terminal multiplexer context, `send-keys -l` is NOT the correct mechanism for
pasting multi-line or large text: tmux parses the string for key names and has quoting
edge cases. The correct mechanism is `load-buffer` + `paste-buffer -p`:

```go
// Load text into tmux's unnamed buffer via stdin (no quoting issues):
loadCmd := exec.Command("tmux", "load-buffer", "-")
loadCmd.Stdin = strings.NewReader(text)
loadCmd.Run()

// Paste with -p (bracketed paste mode) so the target app receives markers:
exec.Command("tmux", "paste-buffer", "-t", target, "-d", "-p").Run()
```

`-p` wraps the pasted content with ESC[200~/ESC[201~ when delivered to the target pane,
which is important when the target application (e.g., Claude Code) itself respects
bracketed paste mode.

`-d` deletes the buffer after pasting (prevents accumulation of large buffers in tmux).

---

## 7. System clipboard access in Go on macOS

### atotto/clipboard

```go
import "github.com/atotto/clipboard"

text, err := clipboard.ReadAll()   // calls pbpaste internally
err = clipboard.WriteAll("text")   // calls pbcopy internally
```

Internally calls `pbpaste` / `pbcopy` via `os/exec`. Text-only, UTF-8 only.
Status: unmaintained since 2022 (v0.1.4), but stable and widely used.

Limitation: does not work in SSH sessions without pasteboard forwarding.

### golang.design/x/clipboard

```go
import "golang.design/x/clipboard"

clipboard.Init()
text := clipboard.Read(clipboard.FmtText)
clipboard.Write(clipboard.FmtText, []byte("text"))
```

Actively maintained, supports images (PNG), provides a `Watch` channel for monitoring
clipboard changes. Requires CGo on macOS (links against AppKit).

### OSC 52: terminal-mediated clipboard

For SSH and tmux contexts where `pbpaste`/`pbcopy` are unavailable, OSC 52 is the
standard:

```go
import "github.com/aymanbagabas/go-osc52/v2"

// Write to clipboard (widely supported):
osc52.New("text to copy").WriteTo(os.Stdout)

// Read from clipboard (requires terminal query support):
// Terminal responds with ESC]52;c;<base64data>BEL
// Less universally supported than write.
```

For tmux, use `TmuxMode` or set `allow-passthrough on` in tmux config:

```go
osc52.New("text").SetMode(osc52.TmuxMode).WriteTo(os.Stdout)
```

OSC 52 is supported by: Alacritty, kitty, iTerm2, WezTerm, tmux (with
`set-clipboard on`), Ghostty.

Sources:
- [aymanbagabas/go-osc52](https://github.com/aymanbagabas/go-osc52)
- [atotto/clipboard](https://github.com/atotto/clipboard)
- [golang-design/clipboard](https://github.com/golang-design/clipboard)

---

## 8. bubbletea approach (reference)

bubbletea (charmbracelet) enables bracketed paste via:

```go
p := tea.NewProgram(model, tea.WithANSICompressor())
p.EnableBracketedPaste()
```

When a paste arrives, the model receives a `tea.PasteMsg` containing the full pasted
string as a single message. This is the cleanest API but requires bubbletea as the
framework. The bubbletea implementation also uses `EnablePaste()` from termenv under
the hood, which writes `\e[?2004h`.

Reference: [bubbletea issue #404](https://github.com/charmbracelet/bubbletea/issues/404)

---

## 9. tview approach (reference)

rivo/tview uses a clipboard injection API rather than terminal bracketed paste:

```go
textarea.SetClipboard(
    func(text string)       { /* write to OS clipboard */ },
    func() string           { return /* read from OS clipboard */ },
)
```

When no custom clipboard is set, tview uses an internal buffer local to the textarea
instance. Ctrl+V calls the `pasteFromClipboard` function and inserts the result at the
cursor. Tview's parent `Application` can also call `EnablePaste()` to enable terminal
bracketed paste mode, which feeds a `PasteMsg`-equivalent into the textarea.

---

## 10. Best practices summary

| Concern | Best Practice |
|---|---|
| Enable bracketed paste | Call `screen.EnablePaste()` (tcell) or equivalent at startup |
| Primary detection | Check `gui.IsPasting` (gocui) on every `Edit()` call |
| Fallback detection | Pattern-match raw ESC[200~/ESC[201~ in the event stream |
| Esc ambiguity | Buffer Esc events; start a ~10 ms timer; flush as standalone Esc if no follow-on arrives |
| Large paste overflow | Watchdog goroutine drains pasteBuf every 200 ms while inPaste is true |
| Delivery to target pane | `tmux load-buffer - | tmux paste-buffer -p -t <pane> -d` |
| System clipboard (local) | `atotto/clipboard` (simple) or `golang.design/x/clipboard` (images, watch) |
| System clipboard (SSH/remote) | OSC 52 via `aymanbagabas/go-osc52` |
| Concurrency | Mutex guards `inPaste`, `nativePaste`, `pasteBuf`; Esc state machine lives on event-loop goroutine only |

---

## Key References

- [gdamore/tcell paste.go](https://github.com/gdamore/tcell/blob/main/paste.go) — EventPaste implementation
- [tcell issue #120](https://github.com/gdamore/tcell/issues/120) — bracketed paste design discussion
- [jesseduffield/lazygit PR #4234](https://github.com/jesseduffield/lazygit/pull/4234) — gocui IsPasting implementation
- [charmbracelet/bubbletea issue #404](https://github.com/charmbracelet/bubbletea/issues/404) — bubbletea bracketed paste design
- [tmux issue #280](https://github.com/tmux/tmux/issues/280) — bracketed paste per-pane scoping
- [cirw.in bracketed paste blog](https://cirw.in/blog/bracketed-paste) — protocol reference
- [aymanbagabas/go-osc52](https://pkg.go.dev/github.com/aymanbagabas/go-osc52/v2) — OSC 52 Go library
- [atotto/clipboard](https://github.com/atotto/clipboard) — pbpaste/pbcopy wrapper
- [golang-design/clipboard](https://github.com/golang-design/clipboard) — cross-platform with image support
- [tmux MacOSX pasteboard workaround](https://github.com/ChrisJohnsen/tmux-MacOSX-pasteboard) — reattach-to-user-namespace history
