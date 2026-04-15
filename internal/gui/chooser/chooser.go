// Package chooser is a gocui-independent list chooser component.
//
// It converts a list of items plus a cursor position into display lines,
// letting callers pipe those lines into a gocui view via fmt.Fprint.
// Keeping this logic as pure functions lets it be unit-tested without
// spinning up a terminal.
package chooser

// Item is a single row in the chooser list.
type Item struct {
	Label   string // display text
	Default bool   // whether this row should carry the default mark
	Data    any    // opaque payload for the caller
}

// State is the UI state of the chooser.
type State struct {
	Items  []Item
	Cursor int
}

const (
	cursorOn   = "\u25b8 " // ▸ followed by a space
	cursorOff  = "  "
	defaultOn  = "* "
	defaultOff = "  "
)

// Render converts state into one display line per item.
// Each line is "<cursorMark><defaultMark><label>".
// cursorMark is "▸ " on the cursor row and "  " elsewhere.
// defaultMark is "* " on items with Default=true and "  " elsewhere.
// width is accepted for future truncation support and is currently ignored.
func Render(s State, width int) []string {
	_ = width
	if len(s.Items) == 0 {
		return nil
	}
	lines := make([]string, len(s.Items))
	for i, item := range s.Items {
		cm := cursorOff
		if i == s.Cursor {
			cm = cursorOn
		}
		dm := defaultOff
		if item.Default {
			dm = defaultOn
		}
		lines[i] = cm + dm + item.Label
	}
	return lines
}

// Move shifts the cursor by delta, clamping to [0, len(Items)-1].
// An empty list leaves the cursor at 0.
func Move(s *State, delta int) {
	if s == nil {
		return
	}
	n := len(s.Items)
	if n == 0 {
		s.Cursor = 0
		return
	}
	next := s.Cursor + delta
	if next < 0 {
		next = 0
	}
	if next > n-1 {
		next = n - 1
	}
	s.Cursor = next
}

// IndexOfDefault returns the index of the first item with Default=true,
// or 0 when no item is marked default (including when items is empty).
func IndexOfDefault(items []Item) int {
	for i, item := range items {
		if item.Default {
			return i
		}
	}
	return 0
}
