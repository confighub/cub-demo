package agents

import (
	"testing"
)

func TestLineWith(t *testing.T) {
	text := "cub variant promote --change-order x: exit status 1\nFailed: Variant 'us-east-test1' has no live status for release 2 yet\nDetails:\n  more"
	if got := lineWith(text, "Failed:"); got != "Variant 'us-east-test1' has no live status for release 2 yet" {
		t.Errorf("lineWith Failed: = %q", got)
	}
	if got := lineWith("a\nb requires reviewed: 0 of 1\nc", "requires"); got != "b requires reviewed: 0 of 1" {
		t.Errorf("lineWith requires = %q", got)
	}
	if got := lineWith("  one\n two ", "absent"); got != "one two" {
		t.Errorf("lineWith fallback = %q", got)
	}
	if got := lineWith("\x1b[31mFailed: red\x1b[0m", "Failed:"); got != "red" {
		t.Errorf("lineWith strips color: %q", got)
	}
}

func TestCompleted(t *testing.T) {
	if !completed(map[string]any{"Completed": true, "Stage": "prod"}) {
		t.Error("Completed true")
	}
	if !completed(map[string]any{"Stage": "Completed"}) {
		t.Error("Stage Completed")
	}
	if completed(map[string]any{"State": "Released", "Stage": "prod"}) {
		t.Error("released is not completed")
	}
	for _, state := range []string{"Aborted", "Restored", "RestoreReleased"} {
		if !terminalState(state) {
			t.Errorf("%s is terminal", state)
		}
	}
	if terminalState("Released") {
		t.Error("Released is not terminal")
	}
}

func TestPrefixedWriter(t *testing.T) {
	var out stringWriter
	w := prefixed{out: &out, prefix: "[ci] "}
	if _, err := w.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	if out.s != "[ci] one\n[ci] two\n" {
		t.Errorf("got %q", out.s)
	}
}

type stringWriter struct{ s string }

func (w *stringWriter) Write(b []byte) (int, error) { w.s += string(b); return len(b), nil }
