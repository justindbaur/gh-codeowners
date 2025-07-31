package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockPrompter struct{ mock.Mock }

func (p *mockPrompter) Input(prompt, defaultValue string) (string, error) {
	args := p.Called(prompt, defaultValue)
	return args.String(0), args.Error(1)
}

func (p *mockPrompter) Select(prompt, defaultValue string, options []string) (int, error) {
	args := p.Called(prompt, defaultValue, options)
	return args.Int(0), args.Error(1)
}

func (p *mockPrompter) Confirm(prompt string, defaultValue bool) (bool, error) {
	args := p.Called(prompt, defaultValue)
	return args.Bool(0), args.Error(1)
}

func newMockPrompter() *mockPrompter {
	return &mockPrompter{}
}

// TODO: Many more tests
func TestFormatString(t *testing.T) {
	// Arrange
	p := newMockPrompter()

	p.On("Input", "Enter a 'Display Name' for team 'test-slug'", "").Return("Something", nil)

	options := &FormatOptions{
		TeamSlug:          "test-slug",
		NumberOfFiles:     2,
		Files:             "README.md\nmain.go",
		TemplateVariables: map[string]string{},
	}

	// Act
	output, err := formatString(p, "Format for team: {slug} and custom {Display Name} and {Display Name}", options)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "Format for team: test-slug and custom Something and Something", output)
	p.AssertNumberOfCalls(t, "Input", 1)
}


func TestFindPrefixLength(t *testing.T) {
    tests := []struct{
    	name string
    	values []string
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
    		name: "No values",
    		values: []string{},
    		expected: 0,
    	},
    	{
    		name: "Single value",
    		values: []string{"@my-org/my-team"},
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
	tests := []struct{
		name string
		input []string
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
		name string
		teams []string
		expected map[string]string
	}{
		{
			name: "Simple",
			teams: []string{"@my-org/team-one-dev", "@my-org/team-a-longer-name-dev"},
			expected: map[string]string{
				"@my-org/team-one-dev": "one",
				"@my-org/team-a-longer-name-dev": "a-longer-name",
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
