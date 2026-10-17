package config

import (
	"reflect"
	"testing"
)

func TestCommandGroupsFromEnv(t *testing.T) {
	t.Setenv("WRAPPER_GATE_COMMAND_GROUPS", "bd sync,/usr/bin/jj git push")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"bd", "sync"}, {"/usr/bin/jj", "git", "push"}}
	if !reflect.DeepEqual(cfg.GateCommandGroups, want) {
		t.Fatalf("command groups = %v, want %v", cfg.GateCommandGroups, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCommandGroupsRejectMalformedEnv(t *testing.T) {
	for _, value := range []string{"bd sync,", ",jj git push", "bd sync,,jj git push", "   "} {
		t.Setenv("WRAPPER_GATE_COMMAND_GROUPS", value)
		if _, err := FromEnv(); err == nil {
			t.Fatalf("expected malformed command group error for %q", value)
		}
	}
}

func TestParseCommandGroup(t *testing.T) {
	group, err := ParseGateCommandGroup("  bd\t sync  ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bd", "sync"}
	if !reflect.DeepEqual(group, want) {
		t.Fatalf("group = %v, want %v", group, want)
	}
	if _, err := ParseGateCommandGroup(" \t "); err == nil {
		t.Fatal("empty command group was accepted")
	}
}

func TestCommandGroupsRejectCommaArguments(t *testing.T) {
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	cfg.GateCommandGroups = [][]string{{"tool", "--label=a,b"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("comma argument was accepted")
	}
}

func TestCommandGroupsRejectInvalidExecutables(t *testing.T) {
	for _, groups := range [][][]string{{nil}, {{""}}, {{"./bd"}}, {{"tools/bd", "sync"}}} {
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		cfg.GateCommandGroups = groups
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected invalid executable error for %v", groups)
		}
	}
}
