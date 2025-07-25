package main

import (
	"bytes"
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

func TestMainCoreReport(t *testing.T) {
	stdin := bytes.NewBuffer([]byte{})
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	m := &mock.Mock{}
	p := newMockPrompter()

	options := &rootCmdOptions{
		In:  stdin,
		Out: stdout,
		Err: stderr,
		ReadFile: func(filePath string) (*File, error) {
			args := m.MethodCalled("ReadFile", filePath)
			return args.Get(0).(*File), args.Error(1)
		},
		GitExec: func(arg ...string) ([]byte, error) {
			args := m.MethodCalled("GitExec", arg)
			return args.Get(0).([]byte), args.Error(1)
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			args := m.MethodCalled("GhExec", arg)
			return args.Get(0).(bytes.Buffer), args.Get(1).(bytes.Buffer), args.Error(2)
		},
		Prompter: p,
	}

	m.On("ReadFile", ".github/CODEOWNERS").Return(&File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	m.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	err := mainCore(options, []string{"report"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "@team-1: 1\n", stdout.String())
}

func TestMainCoreStage(t *testing.T) {
	stdin := bytes.NewBuffer([]byte{})
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	m := &mock.Mock{}
	p := newMockPrompter()

	options := &rootCmdOptions{
		In:  stdin,
		Out: stdout,
		Err: stderr,
		ReadFile: func(filePath string) (*File, error) {
			args := m.MethodCalled("ReadFile", filePath)
			return args.Get(0).(*File), args.Error(1)
		},
		GitExec: func(arg ...string) ([]byte, error) {
			args := m.MethodCalled("GitExec", arg)
			return args.Get(0).([]byte), args.Error(1)
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			args := m.MethodCalled("GhExec", arg)
			return args.Get(0).(bytes.Buffer), args.Get(1).(bytes.Buffer), args.Error(2)
		},
		Prompter: p,
	}

	m.On("ReadFile", ".github/CODEOWNERS").Return(&File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	m.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	m.On("GitExec", []string{"add", "test-dir/test-file.txt"}).Return([]byte{}, nil)

	err := mainCore(options, []string{"stage", "@team-1"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "Staged: test-dir/test-file.txt\n", stdout.String())
}

func TestMainCoreAutoPR(t *testing.T) {
	stdin := bytes.NewBuffer([]byte{})
	stdout := bytes.NewBuffer([]byte{})
	stderr := bytes.NewBuffer([]byte{})
	m := &mock.Mock{}
	p := newMockPrompter()

	options := &rootCmdOptions{
		In:  stdin,
		Out: stdout,
		Err: stderr,
		ReadFile: func(filePath string) (*File, error) {
			args := m.MethodCalled("ReadFile", filePath)
			return args.Get(0).(*File), args.Error(1)
		},
		GitExec: func(arg ...string) ([]byte, error) {
			args := m.MethodCalled("GitExec", arg)
			return args.Get(0).([]byte), args.Error(1)
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			args := m.MethodCalled("GhExec", arg)
			return args.Get(0).(bytes.Buffer), args.Get(1).(bytes.Buffer), args.Error(2)
		},
		AskOne: func(templateContents string, contents any) error {
			args := m.MethodCalled("AskOne", templateContents, contents)
			return args.Error(0)
		},
		Prompter: p,
	}

	p.Mock.On("Input", "Enter branch template", "").Return("branch-{Branch Safe Team Name}", nil)
	p.Mock.On("Input", "Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md").Return("./.github/PULL_REQUEST_TEMPLATE.md", nil)
	p.Mock.On("Input", "Enter the commit message you want to use", "").Return("Do work for {Branch Safe Team Name}", nil)
	p.Mock.On("Input", "Enter a 'Branch Safe Team Name' for team '@team-1'", "").Return("one", nil).Once()

	m.On("ReadFile", ".github/CODEOWNERS").Return(&File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	m.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&File{
		Reader: bytes.NewBufferString("My PR template!"),
		Close:  func() error { return nil },
	}, nil)

	m.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	m.On("GitExec", []string{"add", "test-dir/test-file.txt"}).Return([]byte{}, nil)

	m.On("GitExec", []string{"checkout", "-b", "branch-one"}).Return([]byte{}, nil)
	m.On("GitExec", []string{"commit", "--message", "Do work for one"}).Return([]byte{}, nil)
	m.On("GitExec", []string{"push", "--set-upstream", "origin", "branch-one"}).Return([]byte{}, nil)

	m.On("AskOne", "My PR template!", mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "My PR template!\nFor {slug}: {Branch Safe Team Name}"
	}).Return(nil)

	m.On("GhExec", mock.MatchedBy(func(cmdArgs []string) bool {
		return cmdArgs[0] == "pr" && cmdArgs[1] == "new" && cmdArgs[2] == "--body-file" && cmdArgs[3] != "" && cmdArgs[4] == "--title" && cmdArgs[5] == "Do work for one" && cmdArgs[6] == "--draft"
	})).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	m.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)

	err := mainCore(options, []string{"auto-pr"})

	// Assert things
	assert.NoError(t, err)
}
