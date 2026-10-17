package config

import (
	"reflect"
	"testing"
)

func TestCommandGroupsFromEnv(t *testing.T) {
	t.Setenv("WRAPPER_GATE_COMMAND_GROUPS", `[["bd","sync"],["/usr/bin/jj","git","push"]]`)
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
	for _, value := range []string{`bd sync`, `["bd"]`, `[[42]]`, `[["bd"]] trailing`} {
		t.Setenv("WRAPPER_GATE_COMMAND_GROUPS", value)
		if _, err := FromEnv(); err == nil {
			t.Fatalf("expected malformed command group error for %q", value)
		}
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
