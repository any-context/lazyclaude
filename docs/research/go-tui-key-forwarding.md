# Go TUI Key Forwarding: Space Key Dispatch and Subprocess Input

## Summary

This document answers how space and other keys are dispatched in gocui (jesseduffield fork, which uses tcell), and how Go TUI applications forward all keyboard input to subprocesses or tmux panes. The central finding: **in jesseduffield/gocui, space arrives at `Edit()` as `Key=KeySpace` (value 32) with `ch=0`**, not as `ch=' '` with `Key=0`. This means the common `ch != 0` guard in Editor implementations does NOT catch space - a dedicated `key == KeySpace` case is required.

---

## 1. How Space Is Dispatched Through the Stack

### tcell layer (gdamore/tcell)

In tcell's `NewEventKey`, printable ASCII characters are sent as `KeyRune` events:

- Control characters (ASCII 0-31) and DEL (127) are converted to named Key constants.
- Space (ASCII 32) does NOT meet the condition `ch < ' '` (less than 32), so it is **not treated as a special key** by tcell itself.
- tcell emits `EventKey{Key: KeyRune, Rune: ' '}` for space.

Reference: [tcell/key.go](https://github.com/gdamore/tcell/blob/main/key.go)

### jesseduffield/gocui tcell_driver.go layer

gocui's `tcell_driver.go` adds a **special case** that transforms the space rune into a key code before it reaches any Editor:

```go
if k == tcell.KeyRune {
    k = 0                // clear key - it's a rune
    ch = tev.Rune()
    if ch == ' ' {
        // special handling for spacebar
        k = 32           // tcell keys end at 31 or start at 256
        ch = rune(0)     // clear rune
    }
}
```

**Result:** Space is converted from `(Key=KeyRune, ch=' ')` to `(Key=32, ch=0)`.

In gocui's type system, `KeySpace = 32` (the constant value equals ASCII 32, sitting between tcell's key range ending at 31 and starting at 256).

Reference: [jesseduffield/gocui tcell_driver.go](https://github.com/jesseduffield/gocui/blob/master/tcell_driver.go)

### Edit() signature: what arrives

```go
// Editor interface
type Editor interface {
    Edit(v *View, key Key, ch rune, mod Modifier) bool
}
```

For a space keystroke:
- `key = KeySpace` (value 32)
- `ch = 0` (zero rune)
- `mod = 0`

For a regular letter like 'a':
- `key = 0`
- `ch = 'a'` (rune 97)
- `mod = 0`

### Critical consequence for Edit() implementations

A forwarding Editor that checks `ch != 0` first will **miss space entirely** because ch=0 for space. Space must be handled by a separate `key == KeySpace` case:

```go
func (e *MyEditor) Edit(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
    switch {
    case ch != 0 && mod == 0:
        // handles all printable chars EXCEPT space
        sendToPane(string(ch))
        return true
    case key == gocui.KeySpace:
        // space MUST be caught here - it never reaches ch != 0 branch
        sendToPane(" ")
        return true
    // ... other special keys
    }
    return false
}
```

Reference: [jesseduffield/gocui edit.go](https://github.com/jesseduffield/gocui/blob/master/edit.go) - the `SimpleEditor` uses exactly this pattern with `case key == KeySpace: v.TextArea.TypeCharacter(" ")`.

---

## 2. jroimartin/gocui (termbox-go) vs jesseduffield/gocui (tcell)

The original `jroimartin/gocui` uses **termbox-go** as its terminal backend. The behavior differs:

In termbox-go's `extract_event`, space (ASCII 32) falls into the condition `Key(inbuf[0]) <= KeySpace` which dispatches it as `event.Key = KeySpace, event.Ch = 0`. The jesseduffield fork achieves the same final result via tcell_driver.go's explicit conversion. Both forks deliver space as `(Key=KeySpace, ch=0)` to the Editor.

Reference: [jroimartin/gocui](https://github.com/jroimartin/gocui), [nsf/termbox-go](https://github.com/nsf/termbox-go)

---

## 3. How Lazygit Handles Subprocess Key Input

Lazygit uses jesseduffield/gocui as its TUI framework. For interactive subprocesses (e.g., git add -p, git rebase -i, vim):

**Architecture:**
- Lazygit suspends its own TUI (stops tcell event loop)
- Sets subprocess stdin/stdout/stderr directly to `os.Stdin / os.Stdout / os.Stderr`
- Waits for the subprocess to exit
- Resumes the TUI after the subprocess returns

```go
// Conceptual pattern from lazygit
subprocess.Stdin  = os.Stdin
subprocess.Stdout = os.Stdout
subprocess.Stderr = os.Stderr
subprocess.Run()
// resume TUI
```

This approach bypasses key forwarding entirely: the terminal is handed directly to the subprocess. The gocui/tcell layer is not involved during subprocess execution.

For non-interactive commands (log output, status), lazygit uses pipe-based output capture via `StdoutPipe()` through its `ViewBufferManager`.

References:
- [lazygit tasks_adapter.go](https://github.com/jesseduffield/lazygit/blob/master/pkg/gui/tasks_adapter.go)
- [lazygit subprocess issue #3903](https://github.com/jesseduffield/lazygit/issues/3903)

---

## 4. tmux send-keys and the Space Key

tmux `send-keys` handles space in two ways:

1. **Named key:** `tmux send-keys "Space"` - recognized as the space keycode via `key_string_lookup_string()`.
2. **Literal:** `tmux send-keys -l " "` - sent as UTF-8 byte 0x20 through the literal character processing path.

The `-l` flag disables key name lookup. Without `-l`, space between arguments is consumed by the shell; the key name `"Space"` is needed to explicitly send the spacebar.

Reference: [tmux cmd-send-keys.c](https://github.com/tmux/tmux/blob/master/cmd-send-keys.c)

---

## 5. tcell Space Key: Final Confirmation

tcell dispatches space as `KeyRune` with `Rune()=' '`. The condition `ch < ' '` in `NewEventKey` explicitly excludes space from special-key treatment. Space is a printable character in tcell's model.

However, because jesseduffield/gocui's `tcell_driver.go` adds its own special-case conversion (`if ch == ' ' { k = 32; ch = 0 }`), by the time an event reaches a gocui `Editor.Edit()`, space has already been transformed to `(Key=32=KeySpace, ch=0)`.

---

## 6. Key Forwarding Patterns in Go TUI Applications

### Pattern A: TUI Suspension (lazygit approach)
- Stop the TUI event loop
- Connect subprocess stdin/stdout directly to os.Stdin/os.Stdout
- All terminal input goes to subprocess natively
- Resume TUI after subprocess exits
- Advantage: zero key translation issues, all keys work correctly
- Disadvantage: cannot display TUI UI simultaneously with subprocess output

### Pattern B: PTY-based forwarding
- Allocate a PTY for the subprocess
- Read raw bytes from the terminal (set terminal to raw mode)
- Write raw bytes to the PTY master
- Advantage: can render subprocess output inside TUI panes
- Disadvantage: complex; must handle terminal resize, escape sequences

### Pattern C: tmux send-keys forwarding
- Read keys in the TUI Editor
- Translate Key/ch values back to tmux key names or literal bytes
- Call `tmux send-keys` to inject into target pane
- Space mapping: `Key=KeySpace` (ch=0) must map to `"Space"` or `" "` literal
- Other special keys: must maintain a mapping table (e.g., `KeyArrowUp -> "Up"`)
- Printable chars: `ch != 0` -> send as literal string

#### Special key name map for tmux send-keys (Pattern C)

| gocui Key constant | tmux key name |
|---|---|
| KeySpace (32) | Space |
| KeyEnter | Enter |
| KeyBackspace / KeyBackspace2 | BSpace |
| KeyArrowUp | Up |
| KeyArrowDown | Down |
| KeyArrowLeft | Left |
| KeyArrowRight | Right |
| KeyEsc | Escape |
| KeyTab | Tab |
| KeyDelete | DC (or Delete) |
| KeyHome | Home |
| KeyEnd | End |
| KeyPgup | PPage |
| KeyPgdn | NPage |
| KeyF1-F12 | F1-F12 |

Reference: [tmux valid keys - Baeldung](https://www.baeldung.com/linux/tmux-keys), [tmux send-keys guide](https://tmuxai.dev/tmux-send-keys/)

---

## Key References

- [jesseduffield/gocui](https://github.com/jesseduffield/gocui) - gocui fork used by lazygit (tcell backend)
- [jroimartin/gocui](https://github.com/jroimartin/gocui) - original gocui (termbox-go backend)
- [gdamore/tcell key.go](https://github.com/gdamore/tcell/blob/main/key.go) - tcell key event dispatch
- [nsf/termbox-go](https://github.com/nsf/termbox-go) - original termbox-go
- [jesseduffield/lazygit](https://github.com/jesseduffield/lazygit) - reference TUI application
- [tmux cmd-send-keys.c](https://github.com/tmux/tmux/blob/master/cmd-send-keys.c) - tmux key injection source
