package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindPrefixLength(t *testing.T) {
	tests := []struct {
		name     string
		values   []string
		expected int
	}{
		{
			name: "Simple",
			values: []string{
				"@my-org/team-one-dev",
				"@my-org/team-two-dev",
			},
			expected: 13,
		},
		{
			name: "First is longer",
			values: []string{
				"@my-org/team-a-really-long-name",
				"@my-org/team-two",
			},
			expected: 13,
		},
		{
			name:     "No values",
			values:   []string{},
			expected: 0,
		},
		{
			name:     "Single value",
			values:   []string{"@my-org/my-team"},
			expected: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := findPrefixLength(tt.values)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestReverseStrings(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		output []string
	}{
		{
			name: "Test",
			input: []string{
				"@my-org/team-one-dev",
				"@my-org/team-two-dev",
			},
			output: []string{
				"ved-eno-maet/gro-ym@",
				"ved-owt-maet/gro-ym@",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := reverseStrings(tt.input)
			assert.Equal(t, tt.output, actual)
		})
	}
}

func TestBuildShortNames(t *testing.T) {
	tests := []struct {
		name     string
		teams    []string
		expected map[string]string
	}{
		{
			name:  "Simple",
			teams: []string{"@my-org/team-one-dev", "@my-org/team-a-longer-thing-dev"},
			expected: map[string]string{
				"@my-org/team-one-dev":            "one",
				"@my-org/team-a-longer-thing-dev": "a-longer-thing",
			},
		},
		{
			name: "No common prefix",
			teams: []string{
				"one-team",
				"two-team",
			},
			expected: map[string]string{
				"one-team": "one",
				"two-team": "two",
			},
		},
		{
			name: "No suffix",
			teams: []string{
				"team-one",
				"team-two",
			},
			expected: map[string]string{
				"team-one": "one",
				"team-two": "two",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := buildShortNames(tt.teams)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
