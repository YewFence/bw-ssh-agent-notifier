package process

import (
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestCommandScopesStripsWrappers(t *testing.T) {
	info := Info{
		PID: 42, StartTime: 100, Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "git@example.test"},
		Parents: []Summary{
			{PID: 41, StartTime: 90, Exe: "/usr/bin/git", Cmdline: []string{"git", "fetch"}},
			{PID: 40, StartTime: 80, Exe: "/usr/bin/bd", Cmdline: []string{"bd", "sync"}},
			{PID: 39, StartTime: 70, Exe: "/usr/bin/zsh"},
			{PID: 38, StartTime: 60, Exe: "/usr/bin/zellij"},
			{PID: 37, StartTime: 50, Exe: "/usr/bin/foot"},
			{PID: 36, StartTime: 40, Exe: "/usr/lib/systemd/systemd"},
			{PID: 35, Exe: "/usr/bin/unknown"},
		},
	}
	scopes := CommandScopes(info)
	want := []Summary{
		{PID: info.PID, StartTime: info.StartTime, Exe: info.Exe, Cmdline: info.Cmdline},
		info.Parents[0], info.Parents[1],
	}
	if !reflect.DeepEqual(scopes, want) {
		t.Fatalf("CommandScopes() = %+v, want %+v", scopes, want)
	}
}

func TestDefaultScope(t *testing.T) {
	scopes := []Summary{
		{Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "git@example.test"}},
		{Exe: "/usr/bin/git", Cmdline: []string{"git", "push", "origin", "main"}},
		{Exe: "/usr/bin/bd", Cmdline: []string{"bd", "sync", "--verbose"}},
	}
	for _, tc := range []struct {
		name      string
		groups    [][]string
		index     int
		automatic bool
	}{
		{"default requester", nil, 0, false},
		{"executable", [][]string{{"bd"}}, 2, true},
		{"argv prefix", [][]string{{"bd", "sync"}}, 2, true},
		{"absolute executable", [][]string{{"/usr/bin/bd", "sync"}}, 2, true},
		{"nearest wins", [][]string{{"bd"}, {"git"}}, 1, true},
		{"requester can match", [][]string{{"ssh"}}, 0, true},
		{"different executable path", [][]string{{"/opt/bin/bd"}}, 0, false},
		{"argument mismatch", [][]string{{"bd", "sync-all"}}, 0, false},
		{"branch mismatch", [][]string{{"git", "push", "origin", "other"}}, 0, false},
		{"too many arguments", [][]string{{"bd", "sync", "--verbose", "extra"}}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			index, automatic := DefaultScope(scopes, tc.groups)
			if index != tc.index || automatic != tc.automatic {
				t.Fatalf("DefaultScope() = %d, %v; want %d, %v", index, automatic, tc.index, tc.automatic)
			}
		})
	}
}

func TestInstanceLifetime(t *testing.T) {
	command := exec.Command("sleep", "60")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	info, err := Inspect(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	instance := Instance{PID: info.PID, StartTime: info.StartTime}
	if !instance.Alive() {
		t.Fatal("running process instance is not alive")
	}
	reused := instance
	reused.StartTime++
	if reused.Alive() {
		t.Fatal("same PID with different start time must not match")
	}
	if (Instance{PID: os.Getpid()}).Alive() {
		t.Fatal("missing start time must not match")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	if instance.Alive() {
		t.Fatal("exited process instance is still alive")
	}
}
