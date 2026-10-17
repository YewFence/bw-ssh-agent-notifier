package process

import (
	"testing"
)

func TestCommandLinePreservesArgumentBoundariesAndEscapesControls(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"bd", "sync"}, "bd sync"},
		{[]string{"tool", "two words", ""}, `tool "two words" ""`},
		{[]string{"tool", "line\nbreak", "\x1b[2J"}, `tool "line\nbreak" "\x1b[2J"`},
		{[]string{"tool", `quote"here`, `back\slash`}, `tool "quote\"here" "back\\slash"`},
		{[]string{"tool", "中文参数"}, "tool 中文参数"},
	} {
		if got := CommandLine(tc.args); got != tc.want {
			t.Fatalf("CommandLine(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
