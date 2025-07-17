package main

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
