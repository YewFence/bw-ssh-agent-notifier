package process

import (
	"path/filepath"
	"slices"
)

// CommandScopes lists selectable command instances, from the requester toward
// its ancestors. Session infrastructure is context, not an authorization scope.
func CommandScopes(info Info) []Summary {
	chain := append([]Summary{{PID: info.PID, StartTime: info.StartTime, Exe: info.Exe, Cmdline: info.Cmdline}}, info.Parents...)
	scopes := make([]Summary, 0, len(chain))
	for _, command := range chain {
		if command.StartTime == 0 || isWrapperProcess(ProcessName(command)) {
			continue
		}
		scopes = append(scopes, command)
	}
	return scopes
}

// DefaultScope chooses the nearest configured command, or the requester when
// no preset matches. Groups contain an executable followed by an argv prefix.
func DefaultScope(scopes []Summary, groups [][]string) (int, bool) {
	for index, command := range scopes {
		for _, group := range groups {
			executable := command.Exe
			if executable == "" && len(command.Cmdline) > 0 {
				executable = command.Cmdline[0]
			}
			if !filepath.IsAbs(group[0]) {
				executable = filepath.Base(executable)
			}
			if executable != group[0] || len(command.Cmdline) < len(group) {
				continue
			}
			if slices.Equal(command.Cmdline[1:len(group)], group[1:]) {
				return index, true
			}
		}
	}
	return 0, false
}
