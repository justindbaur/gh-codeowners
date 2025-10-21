package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cli/safeexec"
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

	testOpts.Mock.On("GitExec", []string{
		"stash",
		"push",
	}).Return([]byte{}, nil)

	testOpts.Mock.On("GitExec", []string{
		"stash",
		"pop",
	}).Return([]byte{}, nil)

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
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/test.txt\ndir-2/test.txt")

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

	testOpts.MockWorkingDirectory(strings.Split(workingTree, "\n"))

	// For anything else just pretend success
	testOpts.Mock.On("GitExec", mock.Anything).Return([]byte{}, nil)

	testOpts.Mock.On("GhExec", mock.Anything).Return(*bytes.NewBuffer([]byte{}), *bytes.NewBuffer([]byte{}), nil)

	return testOpts
}

type TestFile struct {
	name     string
	contents string
}

type testWriter struct {
	label string
	buf   *bytes.Buffer
	t     *testing.T
}

func (w *testWriter) Write(b []byte) (int, error) {
	w.t.Logf("%s: %s", w.label, string(b))
	return w.buf.Write(b)
}

func NewTestWriter(t *testing.T, label string) *testWriter {
	return &testWriter{
		t:     t,
		label: label,
		buf:   new(bytes.Buffer),
	}
}

func TestMain(t *testing.T) {
	gitBin, err := safeexec.LookPath("git")

	if err != nil {
		t.Fatalf("Could not locate git: %v", err)
	}

	execGit := func(t *testing.T, arg ...string) {
		cmd := exec.Command(gitBin, arg...)
		gitOutput, err := cmd.Output()
		if err != nil {
			assert.Fail(t, string(gitOutput))
		}
		t.Logf("execGit (%s): %s", arg, string(gitOutput))
	}

	tests := []struct {
		name         string
		args         []string
		initialFiles []TestFile
		updatedFiles []TestFile
		expectedErr  string
		preMock      func(m *mock.Mock)
		promptStubs  func(*internal.MockPrompter)
		assertOut    func(t *testing.T, out string)
	}{
		{
			name:        "Simple Report",
			args:        []string{"report"},
			expectedErr: "",
			promptStubs: func(m *internal.MockPrompter) {

			},
			initialFiles: []TestFile{
				{
					name:     ".github/CODEOWNERS",
					contents: "*.txt @my-org/my-team",
				},
				{
					name:     "file.txt",
					contents: "yo!",
				},
			},
			updatedFiles: []TestFile{
				{
					name:     "file.txt",
					contents: "hello!",
				},
			},
			assertOut: func(t *testing.T, out string) {
				assert.Equal(t, "@my-org/my-team: 1\n", out)
			},
		},
		{
			name:        "Auto PR with interactive staging",
			args:        []string{"auto-pr", "--branch", "branch/{{ .Input \"Name\"}}", "--commit", "My Commit {{ .Input \"Name\"}}"},
			expectedErr: "",
			promptStubs: func(mp *internal.MockPrompter) {
				mp.On(
					"Select",
					"Choose where to put 1 unowned files",
					"",
					[]string{"@my-org/one", "@my-org/two", "Separate", "Choose for each"}).Return(3, nil)

				mp.On(
					"MultiSelect",
					"What teams should 'unowned.txt' be put into (can select multiple)",
					[]string{},
					[]string{"@my-org/one", "@my-org/two", "Separate"},
				).Return([]int{0, 1}, nil)

				mp.On(
					"Input",
					"one: Name",
					"",
				).Return("One", nil)

				mp.On(
					"Input",
					"two: Name",
					"",
				).Return("Two", nil)
			},
			initialFiles: []TestFile{
				{
					name:     ".github/CODEOWNERS",
					contents: "one @my-org/one\ntwo @my-org/two\n",
				},
				{
					name:     "unowned.txt",
					contents: "This is the first portion.\n\nStable\n\nStable\n\nStable\n\nThis is the second portion.\n",
				},
			},
			updatedFiles: []TestFile{
				{
					name:     "one/file.txt",
					contents: "1",
				},
				{
					name:     "two/file.txt",
					contents: "2",
				},
				{
					name:     "unowned.txt",
					contents: "This is the first portion\n\nStable\n\nStable\n\nStable\n\nThis is the second portion\n",
				},
			},
			preMock: func(m *mock.Mock) {
				m.On("GitExecInt", mock.MatchedBy(func(args []string) bool {
					return len(args) == 3 && args[0] == "add" && args[1] == "--patch"
				})).Run(func(args mock.Arguments) {
					file := args.Get(0).([]string)[2]
					cmd := exec.Command(gitBin, "add", "--patch", file)
					cmd.Stdin = bytes.NewReader([]byte("y\nn"))
					assert.NoError(t, cmd.Run())
				}).Once().Return(nil)

				m.On("GitExecInt", mock.MatchedBy(func(args []string) bool {
					return len(args) == 3 && args[0] == "add" && args[1] == "--patch"
				})).Run(func(args mock.Arguments) {
					file := args.Get(0).([]string)[2]
					cmd := exec.Command(gitBin, "add", "--patch", file)
					cmd.Stdin = bytes.NewReader([]byte("y"))
					assert.NoError(t, cmd.Run())
				}).Once().Return(nil)
			},
		},
	}

	// TODO: Do global setup

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := t.TempDir()
			t.Logf("TestDir: %s", testDir)
			os.Chdir(testDir)
			execGit(t, "init")

			// I personally use a signing key based gpg signature and this breaks being able to run this test
			// automatically, so I turn it off just for this repo
			execGit(t, "config", "--local", "commit.gpgsign", "false")

			if tt.initialFiles != nil {
				for _, file := range tt.initialFiles {
					fullPath := path.Join(testDir, file.name)
					os.MkdirAll(filepath.Dir(fullPath), os.ModePerm)
					err = os.WriteFile(fullPath, []byte(file.contents), os.ModePerm)
					assert.NoError(t, err)
				}

				execGit(t, "add", ".")
				execGit(t, "commit", "--message", "Initial commit")
			}

			if tt.updatedFiles != nil {
				for _, file := range tt.updatedFiles {
					fullPath := path.Join(testDir, file.name)
					assert.NoError(t, os.MkdirAll(filepath.Dir(fullPath), os.ModePerm))
					assert.NoError(
						t,
						os.WriteFile(fullPath, []byte(file.contents), os.ModePerm),
					)
				}
			}

			testMock := &mock.Mock{}

			// We won't have an actual remote to push to
			testMock.On("GitExec", mock.MatchedBy(func(args []string) bool {
				return len(args) == 4 && args[0] == "push" && args[1] == "--set-upstream" && args[2] == "origin"
			})).Return([]byte{}, nil)

			stdout := NewTestWriter(t, "out")
			stderr := NewTestWriter(t, "err")

			prompter := internal.NewMockPrompter()

			if tt.promptStubs != nil {
				tt.promptStubs(prompter)
			}

			// TODO: Make use genuine file system and git
			// but not gh or stdout, prompter
			opts := &cmd.RootCmdOptions{
				GitExec: func(arg ...string) ([]byte, error) {
					// Allow for an override
					if customIsMethodCallable(testMock, "GitExec", arg) {
						args := testMock.MethodCalled("GitExec", arg)
						return args.Get(0).([]byte), args.Error(1)
					}

					// Genuine git
					cmd := exec.Command(gitBin, arg...)
					cmd.Dir = testDir
					cmd.WaitDelay = time.Duration(10) * time.Second
					output, err := cmd.Output()
					if err != nil {
						t.Logf("git (%s) error: %v\n%s", arg, err, output)
					} else {
						t.Logf("git (%s) success:\n%s", arg, output)
					}

					return output, err
				},
				GitExecInt: func(arg ...string) error {
					args := testMock.MethodCalled("GitExecInt", arg)
					return args.Error(0)
				},
				ReadFile: func(filePath string) (file cmd.File, err error) {
					// Genuine file system
					actualFile, err := os.Open(filePath)

					if err != nil {
						return
					}

					return &RealFile{innerFile: actualFile}, nil
				},
				Out:      stdout,
				Err:      stderr,
				Prompter: prompter,
				GetRemoteName: func() (string, error) {
					// TODO: Do this genuinely?
					return "origin", nil
				},
				GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
					if len(arg) == 8 && arg[0] == "pr" && arg[1] == "new" {
						t.Logf("GhExec: %s", arg)
						return *bytes.NewBufferString("https://github.com/fake-org/fake-repo/pulls/1"), *bytes.NewBufferString(""), nil
					}

					t.Logf("Unimplemented GhExec: %s", arg)
					return *bytes.NewBufferString(""), *bytes.NewBufferString(""), fmt.Errorf("not implemented")
				},
			}

			if tt.preMock != nil {
				tt.preMock(testMock)
			}

			err = mainCore(opts, tt.args)
			if tt.expectedErr == "" {
				assert.NoError(t, err)

				if tt.assertOut != nil {
					tt.assertOut(t, stdout.buf.String())
				}
			} else {
				assert.Equal(t, tt.expectedErr, err)
			}
		})
	}
}

// Ref: https://github.com/stretchr/testify/issues/1712
func customIsMethodCallable(mock *mock.Mock, method string, arguments ...interface{}) bool {
	for _, call := range mock.ExpectedCalls {
		if call.Method != method {
			continue
		}

		_, diffCount := call.Arguments.Diff(arguments)
		if diffCount != 0 {
			continue
		}

		if call.Repeatability < 0 {
			return false
		}

		return true
	}

	return false
}
