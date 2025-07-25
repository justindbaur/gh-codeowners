package main

import (
	"bytes"
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

	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&cmd.File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	testOpts.Mock.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	err := mainCore(testOpts.toActual(), []string{"report"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "@team-1: 1\n", testOpts.Out.String())
}

func TestMainCoreStage(t *testing.T) {
	testOpts := newTestRootOpts()

	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&cmd.File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	testOpts.Mock.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	testOpts.Mock.On("GitExec", []string{"add", "test-dir/test-file.txt"}).Return([]byte{}, nil)

	err := mainCore(testOpts.toActual(), []string{"stage", "@team-1"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "Staged: test-dir/test-file.txt\n", testOpts.Out.String())
}

func TestMainCoreAutoPR(t *testing.T) {
	testOpts := newTestRootOpts()

	testOpts.Prompter.On("Input", "Enter branch template", "").Return("branch-{Branch Safe Team Name}", nil)
	testOpts.Prompter.On("Input", "Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md").Return("./.github/PULL_REQUEST_TEMPLATE.md", nil)
	testOpts.Prompter.On("Input", "Enter the commit message you want to use", "").Return("Do work for {Branch Safe Team Name}", nil)
	testOpts.Prompter.On("Input", "Enter a 'Branch Safe Team Name' for team '@team-1'", "").Return("one", nil).Once()

	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&cmd.File{
		Reader: bytes.NewBufferString("test-dir @team-1\nother-dir @team-2"),
		Close:  func() error { return nil },
	}, nil)

	testOpts.Mock.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&cmd.File{
		Reader: bytes.NewBufferString("My PR template!"),
		Close:  func() error { return nil },
	}, nil)

	testOpts.Mock.On("GitExec", []string{"--no-pager", "diff", "--name-only"}).Return([]byte("test-dir/test-file.txt"), nil)

	testOpts.Mock.On("GitExec", []string{"add", "test-dir/test-file.txt"}).Return([]byte{}, nil)

	testOpts.Mock.On("GitExec", []string{"checkout", "-b", "branch-one"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"commit", "--message", "Do work for one"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"push", "--set-upstream", "origin", "branch-one"}).Return([]byte{}, nil)

	testOpts.Mock.On("AskOne", "My PR template!", mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "My PR template!\nFor {slug}: {Branch Safe Team Name}"
	}).Return(nil)

	testOpts.Mock.On("GhExec", mock.MatchedBy(func(cmdArgs []string) bool {
		return cmdArgs[0] == "pr" && cmdArgs[1] == "new" && cmdArgs[2] == "--body-file" && cmdArgs[3] != "" && cmdArgs[4] == "--title" && cmdArgs[5] == "Do work for one" && cmdArgs[6] == "--draft"
	})).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)

	err := mainCore(testOpts.toActual(), []string{"auto-pr"})

	// Assert things
	assert.NoError(t, err)
}
