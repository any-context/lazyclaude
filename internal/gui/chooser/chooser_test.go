package chooser

import (
	"reflect"
	"testing"
)

func TestRender(t *testing.T) {
	t.Parallel()

	items3 := []Item{
		{Label: "alpha"},
		{Label: "beta", Default: true},
		{Label: "gamma"},
	}

	tests := []struct {
		name  string
		state State
		want  []string
	}{
		{
			name:  "empty items returns nil",
			state: State{},
			want:  nil,
		},
		{
			name:  "cursor at head, no defaults",
			state: State{Items: []Item{{Label: "a"}, {Label: "b"}}, Cursor: 0},
			want: []string{
				"\u25b8   a",
				"    b",
			},
		},
		{
			name:  "cursor at middle with default elsewhere",
			state: State{Items: items3, Cursor: 0},
			want: []string{
				"\u25b8   alpha",
				"  * beta",
				"    gamma",
			},
		},
		{
			name:  "cursor at tail",
			state: State{Items: items3, Cursor: 2},
			want: []string{
				"    alpha",
				"  * beta",
				"\u25b8   gamma",
			},
		},
		{
			name:  "cursor on default shows both marks",
			state: State{Items: items3, Cursor: 1},
			want: []string{
				"    alpha",
				"\u25b8 * beta",
				"    gamma",
			},
		},
		{
			name:  "single default only, no cursor mark when not on it",
			state: State{Items: []Item{{Label: "x", Default: true}, {Label: "y"}}, Cursor: 1},
			want: []string{
				"  * x",
				"\u25b8   y",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Render(tc.state, 80)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Render mismatch\nwant: %q\n got: %q", tc.want, got)
			}
		})
	}
}

func TestRenderIgnoresWidth(t *testing.T) {
	t.Parallel()

	state := State{Items: []Item{{Label: "hello"}}, Cursor: 0}
	a := Render(state, 0)
	b := Render(state, 1000)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Render should ignore width for now; got %q vs %q", a, b)
	}
}

func TestMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		items   []Item
		start   int
		delta   int
		wantCur int
	}{
		{"down one", []Item{{Label: "a"}, {Label: "b"}, {Label: "c"}}, 0, 1, 1},
		{"down past end clamps", []Item{{Label: "a"}, {Label: "b"}}, 1, 5, 1},
		{"up one", []Item{{Label: "a"}, {Label: "b"}}, 1, -1, 0},
		{"up past start clamps", []Item{{Label: "a"}, {Label: "b"}}, 0, -3, 0},
		{"delta zero is noop", []Item{{Label: "a"}, {Label: "b"}}, 1, 0, 1},
		{"empty stays at zero", nil, 0, 4, 0},
		{"empty negative stays at zero", nil, 0, -4, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &State{Items: tc.items, Cursor: tc.start}
			Move(s, tc.delta)
			if s.Cursor != tc.wantCur {
				t.Fatalf("cursor = %d, want %d", s.Cursor, tc.wantCur)
			}
		})
	}
}

func TestMoveNilState(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Move(nil) panicked: %v", r)
		}
	}()
	Move(nil, 1)
}

func TestIndexOfDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []Item
		want  int
	}{
		{"empty returns zero", nil, 0},
		{"no default returns zero", []Item{{Label: "a"}, {Label: "b"}}, 0},
		{"single default", []Item{{Label: "a"}, {Label: "b", Default: true}, {Label: "c"}}, 1},
		{"first of multiple defaults wins", []Item{{Label: "a"}, {Label: "b", Default: true}, {Label: "c", Default: true}}, 1},
		{"default at head", []Item{{Label: "a", Default: true}, {Label: "b"}}, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IndexOfDefault(tc.items); got != tc.want {
				t.Fatalf("IndexOfDefault = %d, want %d", got, tc.want)
			}
		})
	}
}
