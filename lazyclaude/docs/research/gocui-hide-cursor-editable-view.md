# Hiding the Terminal Cursor in a gocui Editable View

## Summary

In jesseduffield/gocui (the lazygit fork), `g.Cursor = false` is the correct and sufficient mechanism
to hide the terminal cursor. The draw loop unconditionally calls `Screen.HideCursor()` when that flag
is false. However, the flag interacts with view editability in a non-obvious way: an `Editable = true`
view DOES receive an implicit cursor position update from its `TextArea`, which means if `g.Cursor`
inadvertently becomes `true` anywhere in your code the cursor will snap to the TextArea position and
become visible. The deeper issue is that `Editable = true` is not needed to receive all key events --
and ditching it is the correct architectural solution.

---

## Cursor Visibility: How gocui Controls It

### The draw() loop (gui.go)

Cursor visibility is managed in a single place: the `draw()` function, called once per view per frame
inside `flush()`. The relevant block (from source inspection of jesseduffield/gocui master):

```go
if g.Cursor {
    if curview := g.currentView; curview != nil {
        vMaxX, vMaxY := curview.InnerSize()
        if curview.cx >= 0 && curview.cx < vMaxX &&
           curview.cy >= 0 && curview.cy < vMaxY {
            cx, cy := curview.x0+curview.cx+1, curview.y0+curview.cy+1
            Screen.ShowCursor(cx, cy)
        } else {
            Screen.HideCursor()
        }
    }
} else {
    Screen.HideCursor()   // <-- this IS reached when g.Cursor = false
}
```

Critical observations:

1. `g.Cursor = false` takes the `else` branch and calls `Screen.HideCursor()` unconditionally.
2. This block is inside `draw(v)` which is called in a per-view loop, but it references
   `g.currentView`, not the loop variable `v`. The final call before `Screen.Show()` is what
   matters -- but since the condition depends only on `g.Cursor` (constant per frame), calling it
   N times is equivalent to calling it once.
3. `Screen.Show()` is called after `draw()` completes for all views. tcell flushes the cursor
   state set during draw at that point.

**Conclusion: `g.Cursor = false` does work.** If the cursor still appears, it means `g.Cursor` is
being set back to `true` somewhere, or the view receiving focus is not the view you expect.

---

## Does Editable = true Force Cursor Display?

No, `Editable` has no direct effect on `Screen.ShowCursor()`. However, it has two indirect effects:

1. **TextArea tracks a cursor position.** When the view is Editable and has a TextArea, the TextArea
   maintains a 2D cursor position (`cx`, `cy`) on the View. If `g.Cursor` is later toggled to `true`
   for any reason (e.g., focus change to another view that re-enables it), the cursor will jump to the
   TextArea position.

2. **`updatedCursorAndOrigin()` is called** to keep the cursor within the visible area. This does not
   call `ShowCursor` itself -- it only updates `curview.cx` and `curview.cy`.

The draw block always goes through `g.Cursor` first; `Editable` is never consulted.

---

## Key Event Dispatch: Editable = false vs true

This is the source of confusion. Developers use `Editable = true` to ensure rune keys reach their
handler, but **it is not necessary**.

### The execKeybindings function (gui.go)

```go
func (g *Gui) matchView(v *View, kb *keybinding) bool {
    if v == nil { return false }
    if v.Editable && kb.ch != 0 { return false }  // blocks rune keys on Editable views
    if kb.viewName != v.name { return false }
    return true
}
```

And the global fallback condition in `execKeybindings`:

```go
if globalKb == nil && kb.viewName == "" &&
   ((v != nil && !v.Editable) || (kb.ch == 0 && kb.key != KeyCtrlU && ...)) {
    globalKb = kb
}
```

### Summary of dispatch rules

| Binding type | Editable = true | Editable = false |
|---|---|---|
| View-scoped special key (`KeyArrowUp`, etc.) | Dispatched | Dispatched |
| View-scoped rune key (`'a'`, etc.) | **Blocked** (goes to Editor instead) | Dispatched |
| Global special key (`viewName = ""`) | Dispatched (ch == 0 path) | Dispatched |
| Global rune key (`viewName = ""`) | Dispatched (ch == 0 path skips it; Editor handles) | Dispatched |
| Editor.Edit() | Called as fallback after keybindings | Never called |

### Receiving all key events with Editable = false

**All special keys** (arrows, function keys, ctrl combinations) can be received with
`SetKeybinding("myview", key, gocui.ModNone, handler)` on a non-editable view.

**All rune keys** can be received with `SetKeybinding("myview", 0, gocui.ModNone, handler)` where
the rune is passed as the third argument:
```go
g.SetKeybinding("myview", 0, gocui.ModNone, func(g *Gui, v *View) error {
    // ev.Ch contains the rune -- but this API doesn't expose it directly
    return nil
})
```

**Problem:** gocui's `SetKeybinding` binds to a specific key OR rune, not a catch-all. There is no
wildcard "all runes" binding in the standard API.

### The Editor interface as a catch-all (without cursor)

The `Editor` interface is the only way to receive every keystroke (rune + special) in a single
callback:

```go
type Editor interface {
    Edit(v *View, key Key, ch rune, mod Modifier) bool
}
```

It is only invoked when `v.Editable = true && v.Editor != nil`. This is the core tension: the only
"catch-all" mechanism requires `Editable = true`, which sets `cx`/`cy` on the TextArea.

---

## Solutions

### Option A: Keep Editable = true, force g.Cursor = false every frame (RECOMMENDED)

Set `g.Cursor = false` once at startup and never change it. The draw loop will always call
`HideCursor()`.

```go
g.Cursor = false
// Set your custom editor
v.Editable = true
v.Editor = myEditor  // receives all keystrokes
```

This works as long as no other part of the code sets `g.Cursor = true`. Audit for any
conditional like `g.Cursor = g.currentView.Editable` which lazygit itself uses in some versions.

**Pitfall to check:** In some versions of lazygit's vendored gocui, there is code in the focus
change path that sets `g.Cursor = true` when switching to an Editable view. Search your vendored
copy for `g.Cursor =` assignments.

### Option B: Use Editable = false with per-key SetKeybinding

If your input set is bounded (you know all the keys you need), register each one explicitly:

```go
v.Editable = false
// Register all rune keys individually
for _, ch := range "abcdefghijklmnopqrstuvwxyz..." {
    ch := ch
    g.SetKeybinding("myview", ch, gocui.ModNone, func(g *Gui, v *View) error {
        forwardKey(ch)
        return nil
    })
}
// Register special keys
g.SetKeybinding("myview", gocui.KeyArrowUp, gocui.ModNone, handleArrowUp)
```

This avoids `Editable = true` entirely. `g.Cursor = false` is then fully effective with zero caveats.
Downside: verbose for large key sets.

### Option C: Write raw ANSI to os.Stdout (nuclear option)

If gocui's cursor control is fighting you, write the hide sequence directly. Since tcell owns the
terminal, this may be overwritten on the next `Screen.Show()`. Use only as a last resort and write it
inside an `Update()` callback after `Screen.Show()`:

```go
fmt.Fprint(os.Stdout, "\033[?25l")  // hide cursor
```

This is fragile -- tcell may re-show the cursor on the next frame if `g.Cursor = true`.

### Option D: Patch the vendored gocui (if Cursor = true is set on focus change)

Check your vendored `gui.go` for this pattern (lazygit sets it in some versions):

```go
g.Cursor = curView.Editable
```

If present, change it to:

```go
// Do not re-enable cursor on focus change
```

Or wrap with a guard:

```go
if !g.SuppressCursor {
    g.Cursor = curView.Editable
}
```

And set `g.SuppressCursor = true` (you would add this field to the Gui struct).

---

## Root Cause Diagnosis Checklist

If `g.Cursor = false` is set but the cursor still appears, work through this list:

1. **Search for `g.Cursor = true` or `g.Cursor = ` assignments** in your entire codebase including
   vendored gocui. Run: `grep -rn "\.Cursor\s*=" .`
2. **Check if cursor appears only on the focused view or all views.** If only on focus change, the
   issue is in the focus handler.
3. **Verify `g.Cursor` is still false at render time** by adding a log inside `flush()` or `draw()`.
4. **Check tcell version.** Some tcell versions had bugs where `HideCursor()` was not emitted if
   cursor position had not changed since last frame. Upgrading tcell/v2 resolves this.

---

## Key References

- jesseduffield/gocui source, `gui.go` -- cursor draw logic, `execKeybindings`, `matchView`:
  https://github.com/jesseduffield/gocui/blob/master/gui.go
- jesseduffield/gocui source, `view.go` -- TextArea, Editable, updatedCursorAndOrigin:
  https://github.com/jesseduffield/gocui/blob/master/view.go
- tcell v2 Screen interface, `ShowCursor` / `HideCursor`:
  https://pkg.go.dev/github.com/gdamore/tcell/v2#Screen
- lazygit PR #1518 "fix editor" -- context on Editor/Editable interaction:
  https://github.com/jesseduffield/lazygit/pull/1518
- gocui README (lazygit vendor):
  https://github.com/jesseduffield/lazygit/blob/master/vendor/github.com/jesseduffield/gocui/README.md
