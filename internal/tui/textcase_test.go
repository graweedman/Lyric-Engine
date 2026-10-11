package tui

import "testing"

func TestRotateCase(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"hello", "Hello"},
		{"Hello", "HELLO"},
		{"HELLO", "hello"},
		{"hELLo", "Hello"},
		{"'cause", "'Cause"},
		{"'Cause", "'CAUSE"},
		{"i", "I"},
		{"I", "i"},
		{"don't stop", "Don't stop"},
	} {
		if got, ok := rotateCase(c.in); !ok || got != c.want {
			t.Errorf("rotateCase(%q) = %q, %v; want %q", c.in, got, ok, c.want)
		}
	}
	if _, ok := rotateCase("夜に"); ok {
		t.Error("text without case should not rotate")
	}
}
