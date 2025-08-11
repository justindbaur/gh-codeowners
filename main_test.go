package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/justindbaur/gh-codeowners/cmd"
	"github.com/justindbaur/gh-codeowners/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func toActual(testOpts *internal.TestRootCmdOptions) *cmd.RootCmdOptions {
	return &cmd.RootCmdOptions{
		In:  testOpts.In,
		Out: testOpts.Out,
		Err: testOpts.Err,
		ReadFile: func(filePath string) (cmd.File, error) {
			args := testOpts.Mock.MethodCalled("ReadFile", filePath)
			return args.Get(0).(cmd.File), args.Error(1)
		},
		GitExec: func(arg ...string) ([]byte, error) {
			args := testOpts.Mock.MethodCalled("GitExec", arg)
			return args.Get(0).([]byte), args.Error(1)
		},
		GitExecInt: func(arg ...string) error {
			args := testOpts.Mock.MethodCalled("GitExecInt", arg)
			return args.Error(0)
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

func TestMainCoreReport(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	testOpts.MockWorkingDirectory([]string{
		"test-dir/test-file.txt",
	})

	err := mainCore(toActual(testOpts), []string{"report"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "@team-1: 1\n", testOpts.Out.String())
}

func TestMainCoreStage(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	testOpts.MockWorkingDirectory([]string{
		"test-dir/test-file.txt",
	})

	testOpts.Mock.
		On("GitExec", []string{"add", "test-dir/test-file.txt"}).
		Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{"stage", "@team-1"})

	// Assert things
	assert.NoError(t, err)
	assert.Equal(t, "Staged: test-dir/test-file.txt\n", testOpts.Out.String())
}

func TestMainCoreAutoPR(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.Prompter.On("Input", "What branch template do you want?", "").Return("branch-{{ .Input \"Safe Name\"}}", nil)
	testOpts.Prompter.On("Input", "What commit/PR title template do you want?", "Files for {{ .TeamId }}").Return("Do work for {{ .Input \"Safe Name\" }}", nil)
	testOpts.Prompter.On("Input", "Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md").Return("./.github/PULL_REQUEST_TEMPLATE.md", nil)
	testOpts.MockTemplateHole("1", "Safe Name", "one")
	testOpts.MockTemplateHole("2", "Safe Name", "two")

	testOpts.MockCodeowners([]string{
		"test-dir @team-1",
		"other-dir @team-2",
	})

	tempDir, _ := os.MkdirTemp("", "test")

	testOpts.Mock.On("GitExec", []string{
		"rev-parse",
		"--show-toplevel"}).Return(fmt.Appendf(nil, "%s\n", tempDir), nil)

	testOpts.Mock.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&internal.TestFile{
		Contents: "My PR template!",
	}, nil)

	testOpts.MockWorkingDirectory([]string{
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

	err := mainCore(toActual(testOpts), []string{"auto-pr"})

	// Assert things
	assert.NoError(t, err)
}

func TestMainCoreAutoPR_withArgsMakesTwoPRS(t *testing.T) {
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/test.txt\ndir-2/test.txt\n")

	opts.MockTemplateHole("@team-1", "Team Name", "one")
	opts.MockTemplateHole("@team-2", "Team Name", "two")

	err := mainCore(toActual(opts), []string{"auto-pr", "--draft", "--commit", "commit-{{ .Name }}", "--branch", "branch/{{ .Name }}"})

	opts.Mock.AssertNumberOfCalls(t, "GhExec", 2)

	assert.NoError(t, err)
}

func TestMainCoreAutoPR_help(t *testing.T) {
	opts := setupAutoPRTest("", "")

	err := mainCore(toActual(opts), []string{"auto-pr", "--help"})

	assert.NoError(t, err)
	helpOutput := opts.Out.String()
	assert.NotEmpty(t, helpOutput)
}

func TestMainCoreAutoPR_helpLong(t *testing.T) {
	opts := setupAutoPRTest("", "")

	err := mainCore(toActual(opts), []string{"help", "auto-pr"})

	assert.NoError(t, err)
	helpOutput := opts.Out.String()
	assert.NotEmpty(t, helpOutput)
}

func setupAutoPRTest(codeownersFile string, workingTree string) *internal.TestRootCmdOptions {
	testOpts := internal.NewTestRootOpts()

	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&internal.TestFile{
		Contents: codeownersFile,
	}, nil)

	testOpts.Mock.On("ReadFile", "./.github/PULL_REQUEST_TEMPLATE.md").Return(&internal.TestFile{
		Contents: "My PR template!",
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

func TestMain(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectedErr string
		promptStubs func(*internal.MockPrompter)
	}{
		{
			name:        "Test",
			args:        []string{"report"},
			expectedErr: "",
			promptStubs: func(m *internal.MockPrompter) {

			},
		},
	}

	// TODO: Make use genuine file system and git
	// but not gh or stdout, prompter
	opts := &cmd.RootCmdOptions{}

	// TODO: Do global setup

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO: Do common test setup

			// TODO: Do test specific setup
			err := mainCore(opts, tt.args)
			if tt.expectedErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
}
