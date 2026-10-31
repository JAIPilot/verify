package verify

import (
	"bytes"
	"strings"
	"testing"
)

func TestAddingGeneratedTestRequiresExplicitChoice(t *testing.T) {
	for _, tc := range []struct {
		input, answer string
		accepted      bool
	}{
		{"1\n", "Yes", true},
		{"n\n", "No", true},
		{"", "", false},
	} {
		var output bytes.Buffer
		u := newTerminalUI(strings.NewReader(tc.input), &output)
		answer, ok := u.choose("Add this test file?", []string{"Yes", "No"}, false)
		if answer != tc.answer || ok != tc.accepted {
			t.Errorf("input %q: got %q, %v", tc.input, answer, ok)
		}
	}
}
