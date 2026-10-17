package config

import (
	"fmt"
	"strings"
)

// ParseGateCommandGroup parses one whitespace-separated command argv prefix.
func ParseGateCommandGroup(value string) ([]string, error) {
	group := strings.Fields(value)
	if len(group) == 0 {
		return nil, fmt.Errorf("command group is empty")
	}
	return group, nil
}

// ParseGateCommandGroups parses comma-separated whitespace-separated command
// prefixes. Commas and whitespace cannot be part of an argument.
func ParseGateCommandGroups(value string) ([][]string, error) {
	parts := strings.Split(value, ",")
	groups := make([][]string, 0, len(parts))
	for index, part := range parts {
		group, err := ParseGateCommandGroup(part)
		if err != nil {
			return nil, fmt.Errorf("group %d: %w", index+1, err)
		}
		groups = append(groups, group)
	}
	return groups, nil
}
