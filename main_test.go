package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/justindbaur/gh-codeowners/cmd"
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

type TestRootCmdOptions struct {
	Mock     *mock.Mock
	Prompter *mockPrompter
	In       *bytes.Buffer
	Out      *bytes.Buffer
	Err      *bytes.Buffer
}

func (testOpts *TestRootCmdOptions) mockTemplateHole(team string, name string, value string) *mock.Call {
	return testOpts.Prompter.Mock.On("Input", fmt.Sprintf("%s: %s", team, name), "").Return(value, nil)
}

func (testOpts *TestRootCmdOptions) mockCodeowners(codeownersContent []string) {
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").
		Return(&cmd.File{
			Reader: bytes.NewBufferString(strings.Join(codeownersContent, "\n")),
			Close:  func() error { return nil },
		}, nil)
}

func (testOpts *TestRootCmdOptions) mockWorkingDirectory(files []string) {
	testOpts.Mock.
		On("GitExec", []string{"--no-pager", "diff", "--name-only"}).
		Return([]byte(strings.Join(files, "\n")), nil)
}

func (testOpts *TestRootCmdOptions) toActual() *cmd.RootCmdOptions {
	return &cmd.RootCmdOptions{
		In:  testOpts.In,
		Out: testOpts.Out,
		Err: testOpts.Err,
		ReadFile: func(filePath string) (*cmd.File, error) {
			args := testOpts.Mock.MethodCalled("ReadFile", filePath)
			return args.Get(0).(*cmd.File), args.Error(1)
		},
		GitExec: func(arg ...string) ([]byte, error) {
			args := testOpts.Mock.MethodCalled("GitExec", arg)
			return args.Get(0).([]byte), args.Error(1)
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			args := testOpts.Mock.MethodCalled("GhExec", arg)
			return args.Get(0).(bytes.Buffer), args.Get(1).(bytes.Buffer), args.Error(2)
		},
		Prompter: testOpts.Prompter,
		AskOne: func(templateContents string, contents any) error {
			args := testOpts.Mock.MethodCalled("AskOne", templateContents, contents)
			return args.Error(0)
		},
		GetRemoteName: func() (string, error) {
			args := testOpts.Mock.MethodCalled("GetRemoteName")
			return args.String(0), args.Error(1)
		},
	}
}

func newTestRootOpts() *TestRootCmdOptions {
	return &TestRootCmdOptions{
		In:       bytes.NewBuffer([]byte{}),
		Out:      bytes.NewBuffer([]byte{}),
		Err:      bytes.NewBuffer([]byte{}),
		Mock:     &mock.Mock{},
		Prompter: newMockPrompter(),
	}
}

func TestMainCoreReport(t *testing.T) {
	testOpts := newTestRootOpts()

	testOpts.mockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	testOpts.mockWorkingDirectory([]string{
		"test-dir/test-file.txt",
	})

	err := mainCore(testOpts.toActual(), []string{"report"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "@team-1: 1\n", testOpts.Out.String())
}

func TestMainCoreStage(t *testing.T) {
	testOpts := newTestRootOpts()

	testOpts.mockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	testOpts.mockWorkingDirectory([]string{
		"test-dir/test-file.txt",
	})

	testOpts.Mock.
		On("GitExec", []string{"add", "test-dir/test-file.txt"}).
		Return([]byte{}, nil)

	err := mainCore(testOpts.toActual(), []string{"stage", "@team-1"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "Staged: test-dir/test-file.txt\n", testOpts.Out.String())
}

func TestMainCoreAutoPR(t *testing.T) {
	testOpts := newTestRootOpts()

	testOpts.Prompter.On("Input", "What branch template do you want?", "").Return("branch-{{ .Input \"Safe Name\"}}", nil)
	testOpts.Prompter.On("Input", "What commit/PR title template do you want?", "Files for {{ .Team }}").Return("Do work for {{ .Input \"Safe Name\" }}", nil)
	testOpts.Prompter.On("Input", "Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md").Return("./.github/PULL_REQUEST_TEMPLATE.md", nil)
	testOpts.mockTemplateHole("1", "Safe Name", "one")
	testOpts.mockTemplateHole("2", "Safe Name", "two")

	testOpts.mockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	tempDir, _ := os.MkdirTemp("", "test")

	testOpts.Mock.On("GitExec", []string{
		"rev-parse",
		"--show-toplevel"}).Return(fmt.Appendf(nil, "%s\n", tempDir), nil)

	testOpts.Mock.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&cmd.File{
		Reader: bytes.NewBufferString("My PR template!"),
		Close:  func() error { return nil },
	}, nil)

	testOpts.mockWorkingDirectory([]string{
		"test-dir/test-file.txt",
		"other-dir/file.txt",
	})

	testOpts.Mock.On("GitExec", []string{"add", "test-dir/test-file.txt"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"add", "other-dir/file.txt"}).Return([]byte{}, nil)

	testOpts.Mock.On("GetRemoteName").Return("origin", nil)

	testOpts.Mock.On("GitExec", []string{"checkout", "-b", "branch-one"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-b", "branch-two"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"commit", "--message", "Do work for one"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"commit", "--message", "Do work for two"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"push", "--set-upstream", "origin", "branch-one"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"push", "--set-upstream", "origin", "branch-two"}).Return([]byte{}, nil)

	// TODO: Mock with an actual PR template
	testOpts.Mock.On("AskOne", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "My PR template!\nFor {{ .TeamId }}: {{ .Input \"Safe Name\"}}"
	}).Return(nil)

	testOpts.Mock.On("GhExec", mock.MatchedBy(func(cmdArgs []string) bool {
		return cmdArgs[0] == "pr" && cmdArgs[1] == "new" && cmdArgs[2] == "--body-file" && cmdArgs[3] != "" && cmdArgs[4] == "--title" && cmdArgs[5] == "Do work for one" && cmdArgs[6] == "--draft=false" && cmdArgs[7] == "--dry-run=false"
	})).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	testOpts.Mock.On("GhExec", mock.MatchedBy(func(cmdArgs []string) bool {
		return cmdArgs[0] == "pr" && cmdArgs[1] == "new" && cmdArgs[2] == "--body-file" && cmdArgs[3] != "" && cmdArgs[4] == "--title" && cmdArgs[5] == "Do work for two" && cmdArgs[6] == "--draft=false" && cmdArgs[7] == "--dry-run=false"
	})).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)

	err := mainCore(testOpts.toActual(), []string{"auto-pr"})

	// Assert things
	assert.NoError(t, err)
}

func TestMainCoreAutoPR_withArgsMakesTwoPRS(t *testing.T) {
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/test.txt\ndir-2/test.txt\n")

	opts.mockTemplateHole("@team-1", "Team Name", "one")
	opts.mockTemplateHole("@team-2", "Team Name", "two")

	err := mainCore(opts.toActual(), []string{"auto-pr", "--draft", "--commit", "commit-{Team Name}", "--branch", "branch/{Team Name}"})

	opts.Mock.AssertNumberOfCalls(t, "GhExec", 2)

	assert.NoError(t, err)
}

func TestMainCoreAutoPR_help(t *testing.T) {
	opts := setupAutoPRTest("", "")

	err := mainCore(opts.toActual(), []string{"auto-pr", "--help"})

	assert.NoError(t, err)
	helpOutput := opts.Out.String()
	assert.NotEmpty(t, helpOutput)
}

func TestMainCoreAutoPR_helpLong(t *testing.T) {
	opts := setupAutoPRTest("", "")

	err := mainCore(opts.toActual(), []string{"help", "auto-pr"})

	assert.NoError(t, err)
	helpOutput := opts.Out.String()
	assert.NotEmpty(t, helpOutput)
}

func setupAutoPRTest(codeownersFile string, workingTree string) *TestRootCmdOptions {
	testOpts := newTestRootOpts()

	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&cmd.File{
		Reader: bytes.NewBufferString(codeownersFile),
		Close:  func() error { return nil },
	}, nil)

	testOpts.Mock.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&cmd.File{
		Reader: bytes.NewBufferString("My PR template!"),
		Close:  func() error { return nil },
	}, nil)

	tempDir, _ := os.MkdirTemp("", "test")

	testOpts.Mock.On("GetRemoteName").Return("origin", nil)

	testOpts.Mock.On("GitExec", []string{
		"rev-parse",
		"--show-toplevel"}).Return(fmt.Appendf(nil, "%s\n", tempDir), nil)

	testOpts.Prompter.On("Input", "Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md").Return("./.github/PULL_REQUEST_TEMPLATE.md", nil)

	testOpts.Mock.On("AskOne", "", mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "My PR template!\nFor {slug}: {Team Name}"
	}).Return(nil)

	testOpts.Mock.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte(workingTree), nil)

	// For anything else just pretend success
	testOpts.Mock.On("GitExec", mock.Anything).Return([]byte{}, nil)

	testOpts.Mock.On("GhExec", mock.Anything).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	return testOpts
}
