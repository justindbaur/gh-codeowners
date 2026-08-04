package main

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/justindbaur/gh-codeowners/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ---------------------------------------------------------------------------
// report command
// ---------------------------------------------------------------------------

func TestMainCoreReport_multipleTeams(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"backend/ @org/backend",
		"frontend/ @org/frontend",
	})

	testOpts.MockWorkingDirectory([]string{
		"backend/server.go",
		"backend/handler.go",
		"frontend/app.js",
	})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	out := testOpts.Out.String()
	// Map iteration is non-deterministic, so check each line individually.
	assert.Contains(t, out, "@org/backend: 2\n")
	assert.Contains(t, out, "@org/frontend: 1\n")
}

func TestMainCoreReport_unownedFiles(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
	})

	testOpts.MockWorkingDirectory([]string{
		"main.go",
		"README.md",
		"docs/guide.md",
	})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	out := testOpts.Out.String()
	assert.Contains(t, out, "@org/backend: 1\n")
	assert.Contains(t, out, "Files that are unowned: 2\n")
}

func TestMainCoreReport_multipleOwnersForFile(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"shared/ @org/team-a @org/team-b",
	})

	testOpts.MockWorkingDirectory([]string{
		"shared/utils.go",
	})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	out := testOpts.Out.String()
	// Files with multiple owners are reported individually rather than counted.
	assert.Contains(t, out, "File 'shared/utils.go' is owned by multiple teams @org/team-a, @org/team-b")
	// They should NOT appear in the per-owner count summary.
	assert.NotContains(t, out, "@org/team-a:")
	assert.NotContains(t, out, "@org/team-b:")
}

func TestMainCoreReport_emptyWorkingTree(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
	})

	testOpts.MockWorkingDirectory([]string{})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	assert.Empty(t, testOpts.Out.String())
}

func TestMainCoreReport_missingCodeowners(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	// All three candidate locations fail.
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))
	testOpts.Mock.On("ReadFile", "CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))
	testOpts.Mock.On("ReadFile", "docs/CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))

	testOpts.MockWorkingDirectory([]string{"main.go"})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.ErrorContains(t, err, "could not locate a CODEOWNERS file")
}

func TestMainCoreReport_codeownersInRoot(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	// .github/CODEOWNERS is missing; fall through to root CODEOWNERS.
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))
	testOpts.Mock.On("ReadFile", "CODEOWNERS").Return(&internal.TestFile{
		Contents: "*.go @org/backend\n",
	}, nil)

	testOpts.MockWorkingDirectory([]string{"main.go"})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	assert.Equal(t, "@org/backend: 1\n", testOpts.Out.String())
}

func TestMainCoreReport_codeownersInDocs(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	// Neither .github/ nor root has a CODEOWNERS; fall through to docs/.
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))
	testOpts.Mock.On("ReadFile", "CODEOWNERS").Return(&internal.TestFile{}, fmt.Errorf("not found"))
	testOpts.Mock.On("ReadFile", "docs/CODEOWNERS").Return(&internal.TestFile{
		Contents: "*.go @org/backend\n",
	}, nil)

	testOpts.MockWorkingDirectory([]string{"main.go"})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	assert.Equal(t, "@org/backend: 1\n", testOpts.Out.String())
}

func TestMainCoreReport_mixedOwnedAndUnowned(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
		"*.ts @org/frontend",
	})

	testOpts.MockWorkingDirectory([]string{
		"main.go",
		"app.ts",
		"README.md",  // unowned
		"schema.sql", // unowned
	})

	err := mainCore(toActual(testOpts), []string{"report"})

	assert.NoError(t, err)
	assert.Equal(t, "@org/backend: 1\n@org/frontend: 1\nFiles that are unowned: 2\n", testOpts.Out.String())
}

// ---------------------------------------------------------------------------
// stage command
// ---------------------------------------------------------------------------

func TestMainCoreStage_multipleFiles(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
		"*.js @org/frontend",
	})

	testOpts.MockWorkingDirectory([]string{
		"main.go",
		"server.go",
		"app.js", // owned by a different team
	})

	testOpts.Mock.On("GitExec", []string{"add", "main.go"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"add", "server.go"}).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{"stage", "@org/backend"})

	assert.NoError(t, err)
	out := testOpts.Out.String()
	assert.Contains(t, out, "Staged: main.go\n")
	assert.Contains(t, out, "Staged: server.go\n")
	assert.NotContains(t, out, "app.js")
}

func TestMainCoreStage_noMatchingTeam(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
	})

	testOpts.MockWorkingDirectory([]string{
		"main.go",
	})

	err := mainCore(toActual(testOpts), []string{"stage", "@org/nonexistent"})

	assert.ErrorContains(t, err, "did not find any files owned by '@org/nonexistent'")
}

func TestMainCoreStage_missingArgument(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	err := mainCore(toActual(testOpts), []string{"stage"})

	assert.ErrorContains(t, err, "required team argument missing")
}

func TestMainCoreStage_gitAddError(t *testing.T) {
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"*.go @org/backend",
	})

	testOpts.MockWorkingDirectory([]string{
		"main.go",
	})

	testOpts.Mock.On("GitExec", []string{"add", "main.go"}).
		Return([]byte{}, fmt.Errorf("permission denied"))

	err := mainCore(toActual(testOpts), []string{"stage", "@org/backend"})

	assert.ErrorContains(t, err, "failed to stage 'main.go'")
	assert.ErrorContains(t, err, "permission denied")
}

func TestMainCoreStage_multipleOwnerFile(t *testing.T) {
	// A file with two owners should be staged when targeting either owner.
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{
		"shared/ @org/team-a @org/team-b",
	})

	testOpts.MockWorkingDirectory([]string{
		"shared/util.go",
	})

	testOpts.Mock.On("GitExec", []string{"add", "shared/util.go"}).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{"stage", "@org/team-b"})

	assert.NoError(t, err)
	assert.Contains(t, testOpts.Out.String(), "Staged: shared/util.go\n")
}

// ---------------------------------------------------------------------------
// auto-pr command
// ---------------------------------------------------------------------------

func TestMainCoreAutoPR_noFilesToPR(t *testing.T) {
	// When the working tree is empty there is nothing to make PRs for.
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{"*.go @org/backend"})
	testOpts.MockWorkingDirectory([]string{})

	err := mainCore(toActual(testOpts), []string{"auto-pr"})

	assert.ErrorContains(t, err, "there are no files to make PR's for")
}

func TestMainCoreAutoPR_singleTeamError(t *testing.T) {
	// auto-pr requires at least two teams; a single team should return an error.
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{"*.go @org/backend"})
	testOpts.MockWorkingDirectory([]string{"main.go"})

	err := mainCore(toActual(testOpts), []string{"auto-pr"})

	assert.ErrorContains(t, err, "only one PR would be made")
}

func TestMainCoreAutoPR_dryRun(t *testing.T) {
	// With --dry-run, GhExec should be called with --dry-run=true.
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/test.txt\ndir-2/test.txt")
	opts.MockTemplateHole("@team-1", "Team Name", "one")
	opts.MockTemplateHole("@team-2", "Team Name", "two")

	err := mainCore(toActual(opts), []string{
		"auto-pr", "--dry-run",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.NoError(t, err)

	// Verify GhExec was called twice (once per team) and always with --dry-run=true.
	ghCalls := filterCalls(opts, "GhExec")
	assert.Len(t, ghCalls, 2, "expected two GhExec calls (one per team PR)")
	for _, args := range ghCalls {
		assert.Contains(t, args, "--dry-run=true", "expected --dry-run=true in GhExec args")
		assert.Contains(t, args, "--draft=false", "dry-run should not imply draft")
	}
}

func TestMainCoreAutoPR_draft(t *testing.T) {
	// With --draft, GhExec should be called with --draft=true.
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/test.txt\ndir-2/test.txt")
	opts.MockTemplateHole("@team-1", "Team Name", "one")
	opts.MockTemplateHole("@team-2", "Team Name", "two")

	err := mainCore(toActual(opts), []string{
		"auto-pr", "--draft",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.NoError(t, err)

	ghCalls := filterCalls(opts, "GhExec")
	assert.Len(t, ghCalls, 2, "expected two GhExec calls (one per team PR)")
	for _, args := range ghCalls {
		assert.Contains(t, args, "--draft=true", "expected --draft=true in GhExec args")
		assert.Contains(t, args, "--dry-run=false", "draft should not imply dry-run")
	}
}

func TestMainCoreAutoPR_prAlias(t *testing.T) {
	// The "pr" alias should behave the same as "auto-pr".
	testOpts := internal.NewTestRootOpts()

	testOpts.MockCodeowners([]string{"*.go @org/backend"})
	testOpts.MockWorkingDirectory([]string{"main.go"})

	// With only one team this will fail, but the alias must be recognised
	// (not "unknown command").
	err := mainCore(toActual(testOpts), []string{"pr"})

	assert.ErrorContains(t, err, "only one PR would be made")
}

// filterCalls returns the []string arguments for every recorded call to
// methodName on testOpts.Mock.
func filterCalls(testOpts *internal.TestRootCmdOptions, methodName string) [][]string {
	var result [][]string
	for _, call := range testOpts.Mock.Calls {
		if call.Method != methodName {
			continue
		}
		if args, ok := call.Arguments.Get(0).([]string); ok {
			result = append(result, args)
		}
	}
	return result
}

func hasCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if slices.Equal(call, want) {
			return true
		}
	}

	return false
}

func hasBranchDeleteCall(calls [][]string) bool {
	for _, call := range calls {
		if len(call) == 3 && call[0] == "branch" && call[1] == "-D" && strings.HasPrefix(call[2], "branch/") {
			return true
		}
	}

	return false
}

// ---------------------------------------------------------------------------
// auto-pr: template and remote-name error paths
// ---------------------------------------------------------------------------

func TestMainCoreAutoPR_emptyBranchTemplate(t *testing.T) {
	testOpts := internal.NewTestRootOpts()
	testOpts.MockCodeowners([]string{"dir-1 @team-1", "dir-2 @team-2"})
	testOpts.MockWorkingDirectory([]string{"dir-1/file.txt", "dir-2/file.txt"})

	testOpts.Prompter.On("Input", "What branch template do you want?", "").Return("", nil)

	err := mainCore(toActual(testOpts), []string{"auto-pr", "--commit", "commit-{{ .Name }}"})

	assert.ErrorContains(t, err, "branch template is required")
}

func TestMainCoreAutoPR_emptyCommitTemplate(t *testing.T) {
	testOpts := internal.NewTestRootOpts()
	testOpts.MockCodeowners([]string{"dir-1 @team-1", "dir-2 @team-2"})
	testOpts.MockWorkingDirectory([]string{"dir-1/file.txt", "dir-2/file.txt"})

	testOpts.Prompter.On("Input", "What branch template do you want?", "").Return("branch/{{ .Name }}", nil)
	testOpts.Prompter.On("Input", "What commit/PR title template do you want?", "Files for {{ .TeamId }}").Return("", nil)

	err := mainCore(toActual(testOpts), []string{"auto-pr"})

	assert.ErrorContains(t, err, "commit template is required")
}

func TestMainCoreAutoPR_getRemoteNameError(t *testing.T) {
	testOpts := newAutoPRBaseTestNoRemote(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GetRemoteName").Return("", fmt.Errorf("no remotes configured"))

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "could not determine remote name")
}

// ---------------------------------------------------------------------------
// auto-pr: createPullRequest error paths
// ---------------------------------------------------------------------------

// newAutoPRBaseTest provides the minimum mocks to reach createPullRequest
// without any catch-all git/gh handlers.  Each test adds precise expectations.
func newAutoPRBaseTest(t *testing.T, codeownersContent, workingTree string) *internal.TestRootCmdOptions {
	t.Helper()
	testOpts := internal.NewTestRootOpts()
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").
		Return(&internal.TestFile{Contents: codeownersContent}, nil)
	testOpts.MockWorkingDirectory(strings.Split(workingTree, "\n"))
	tempDir := t.TempDir()
	testOpts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	testOpts.Mock.On("GetRemoteName").Return("origin", nil)
	return testOpts
}

// newAutoPRBaseTestNoRemote is like newAutoPRBaseTest but does NOT register
// GetRemoteName — callers set up their own remote-name mock.
func newAutoPRBaseTestNoRemote(t *testing.T, codeownersContent, workingTree string) *internal.TestRootCmdOptions {
	t.Helper()
	testOpts := internal.NewTestRootOpts()
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").
		Return(&internal.TestFile{Contents: codeownersContent}, nil)
	testOpts.MockWorkingDirectory(strings.Split(workingTree, "\n"))
	tempDir := t.TempDir()
	testOpts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	return testOpts
}

func TestMainCoreAutoPR_gitCheckoutError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, fmt.Errorf("branch already exists"))

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "error checking out branch")
}

func TestMainCoreAutoPR_gitAddError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, fmt.Errorf("path does not exist"))
	testOpts.Mock.On("GitExec", []string{"restore", "--staged", "."}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "branch" && args[1] == "-D" && strings.HasPrefix(args[2], "branch/")
	})).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "problem adding files")
	gitCalls := filterCalls(testOpts, "GitExec")
	assert.True(t, hasCall(gitCalls, []string{"restore", "--staged", "."}))
	assert.True(t, hasCall(gitCalls, []string{"checkout", "-"}))
	assert.True(t, hasBranchDeleteCall(gitCalls))
}

func TestMainCoreAutoPR_interactiveStagingError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt\nextra.txt")
	testOpts.Prompter.On("Select", "Choose where to put 1 unowned files", "", mock.Anything).
		Return(3, nil)
	testOpts.Prompter.On("MultiSelect", mock.Anything, []string{}, []string{"@team-1", "@team-2", "Separate"}).
		Return([]int{0, 1}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExecInt", mock.Anything).Return(fmt.Errorf("patch failed"))
	testOpts.Mock.On("GitExec", []string{"restore", "--staged", "."}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "branch" && args[1] == "-D" && strings.HasPrefix(args[2], "branch/")
	})).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "issue doing interactive staging")
	gitCalls := filterCalls(testOpts, "GitExec")
	assert.True(t, hasCall(gitCalls, []string{"restore", "--staged", "."}))
	assert.True(t, hasCall(gitCalls, []string{"checkout", "-"}))
	assert.True(t, hasBranchDeleteCall(gitCalls))
}

func TestMainCoreAutoPR_gitCommitError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, fmt.Errorf("nothing to commit"))
	testOpts.Mock.On("GitExec", []string{"restore", "--staged", "."}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "branch" && args[1] == "-D" && strings.HasPrefix(args[2], "branch/")
	})).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "problem committing code")
	gitCalls := filterCalls(testOpts, "GitExec")
	assert.True(t, hasCall(gitCalls, []string{"restore", "--staged", "."}))
	assert.True(t, hasCall(gitCalls, []string{"checkout", "-"}))
	assert.True(t, hasBranchDeleteCall(gitCalls))
}

func TestMainCoreAutoPR_gitPushError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "push"
	})).Return([]byte{}, fmt.Errorf("remote rejected"))
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "problem pushing to remote")
	assert.Contains(t, err.Error(), "committed changes were left on branch 'branch/")
	gitCalls := filterCalls(testOpts, "GitExec")
	assert.True(t, hasCall(gitCalls, []string{"checkout", "-"}))
	assert.False(t, hasBranchDeleteCall(gitCalls))
}

func TestMainCoreAutoPR_ghPrCreateError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "push"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)

	testOpts.Mock.On("GhExec", mock.MatchedBy(func(args []string) bool {
		return len(args) > 1 && args[0] == "pr" && args[1] == "new"
	})).Return(*newBuf(""), *newBuf("rate limit exceeded"), fmt.Errorf("gh cli error"))

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "error creating PR with GitHub CLI")
	assert.Contains(t, err.Error(), "branch 'branch/")
	assert.ErrorContains(t, err, "was left in place")
	assert.True(t, hasCall(filterCalls(testOpts, "GitExec"), []string{"checkout", "-"}))
}

func TestMainCoreAutoPR_addRecoveryCheckoutFailure(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, fmt.Errorf("path does not exist"))
	testOpts.Mock.On("GitExec", []string{"restore", "--staged", "."}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, fmt.Errorf("cannot switch branches"))

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "problem adding files")
	assert.ErrorContains(t, err, "recovery failed")
	assert.ErrorContains(t, err, "error trying to checkout base branch")
}

// ---------------------------------------------------------------------------
// auto-pr: non-unique branch names and separate-files PR
// ---------------------------------------------------------------------------

func TestMainCoreAutoPR_nonUniqueBranchAppended(t *testing.T) {
	// When the branch template is static (no team-specific part), both teams would
	// produce the same branch name.  The second one should get a number appended.
	opts := setupAutoPRTest("dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")

	err := mainCore(toActual(opts), []string{
		"auto-pr",
		"--commit", "commit-static",
		"--branch", "branch-static",
	})

	assert.NoError(t, err)
	assert.Contains(t, opts.Out.String(), "is not unique, adding incrementing number")
}

func TestMainCoreAutoPR_separatePRForUnownedFiles(t *testing.T) {
	// Unowned files routed to "Separate" should result in a third PR being created.
	opts := setupAutoPRTest(
		"dir-1 @team-1\ndir-2 @team-2\n",
		"dir-1/file.txt\ndir-2/file.txt\nunowned.txt",
	)

	// Route the unowned file to Separate (index 2 for 2 sorted teams).
	opts.Prompter.On("Select", "Choose where to put 1 unowned files", "", mock.Anything).
		Return(2, nil)

	err := mainCore(toActual(opts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.NoError(t, err)
	// Two team PRs + one separate PR.
	assert.Len(t, filterCalls(opts, "GhExec"), 3)
}

func TestMainCoreAutoPR_gitStashPushError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "push"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte("?? remaining.txt\n"), nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "--include-untracked" && args[3] == "-m" && strings.HasPrefix(args[4], "gh-codeowners:auto-pr:branch/")
	})).
		Return([]byte{}, fmt.Errorf("stash failed"))
	testOpts.Mock.On("GhExec", mock.MatchedBy(func(args []string) bool {
		return len(args) > 1 && args[0] == "pr" && args[1] == "new"
	})).Return(*newBuf("https://example/pr/1"), *newBuf(""), nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "stash failed")
}

func TestMainCoreAutoPR_checkoutBackError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "push"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte("?? remaining.txt\n"), nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "--include-untracked" && args[3] == "-m" && strings.HasPrefix(args[4], "gh-codeowners:auto-pr:branch/")
	})).
		Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"stash", "list", "--format=%gd\t%s"}).
		Return([]byte("stash@{1}\tgh-codeowners:auto-pr:branch/1\nstash@{0}\tgh-codeowners:auto-pr:branch/2\n"), nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, fmt.Errorf("cannot switch branches"))
	testOpts.Mock.On("GhExec", mock.MatchedBy(func(args []string) bool {
		return len(args) > 1 && args[0] == "pr" && args[1] == "new"
	})).Return(*newBuf("https://example/pr/1"), *newBuf(""), nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "error trying to checkout base branch")
}

func TestMainCoreAutoPR_stashPopError(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 3 && args[0] == "checkout" && args[1] == "-b"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "add"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "commit"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return args[0] == "push"
	})).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte("?? remaining.txt\n"), nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "--include-untracked" && args[3] == "-m" && strings.HasPrefix(args[4], "gh-codeowners:auto-pr:branch/")
	})).
		Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", []string{"stash", "list", "--format=%gd\t%s"}).
		Return([]byte("stash@{1}\tgh-codeowners:auto-pr:branch/1\nstash@{0}\tgh-codeowners:auto-pr:branch/2\n"), nil)
	testOpts.Mock.On("GitExec", []string{"checkout", "-"}).Return([]byte{}, nil)
	testOpts.Mock.On("GitExec", mock.MatchedBy(func(args []string) bool {
		return len(args) == 4 && args[0] == "stash" && args[1] == "pop" && args[2] == "--index" && (args[3] == "stash@{0}" || args[3] == "stash@{1}")
	})).
		Return([]byte{}, fmt.Errorf("conflict while applying stash"))
	testOpts.Mock.On("GhExec", mock.MatchedBy(func(args []string) bool {
		return len(args) > 1 && args[0] == "pr" && args[1] == "new"
	})).Return(*newBuf("https://example/pr/1"), *newBuf(""), nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "branch/{{ .Name }}",
	})

	assert.ErrorContains(t, err, "failed to apply stash stash@{")
}

// ---------------------------------------------------------------------------
// auto-pr: template parsing/execution errors
// ---------------------------------------------------------------------------

func TestMainCoreAutoPR_invalidBranchTemplate(t *testing.T) {
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.Anything).Return([]byte{}, nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		"--commit", "commit-{{ .Name }}",
		"--branch", "{{ invalid template",
	})

	assert.ErrorContains(t, err, "error while formatting branch template")
}

func TestMainCoreAutoPR_templateInputPromptError(t *testing.T) {
	// When the branch template calls .Input and the prompter returns an error,
	// createPullRequest should surface it.
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.Anything).Return([]byte{}, nil)

	// Short names for [@team-1, @team-2] are "1" and "2".
	testOpts.Prompter.On("Input", mock.MatchedBy(func(s string) bool {
		return strings.HasSuffix(s, ": PR Name")
	}), "").Return("", fmt.Errorf("user cancelled"))

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		`--branch`, `branch/{{ .Input "PR Name" }}`,
		`--commit`, `commit/{{ .Name }}`,
	})

	assert.ErrorContains(t, err, "error while formatting branch template")
}

func TestMainCoreAutoPR_templateInputEmptyValue(t *testing.T) {
	// When the user provides an empty string for a template input, an error
	// should be returned.
	testOpts := newAutoPRBaseTest(t, "dir-1 @team-1\ndir-2 @team-2\n", "dir-1/file.txt\ndir-2/file.txt")
	testOpts.Mock.On("GitExec", mock.Anything).Return([]byte{}, nil)

	testOpts.Prompter.On("Input", mock.MatchedBy(func(s string) bool {
		return strings.HasSuffix(s, ": PR Name")
	}), "").Return("", nil)

	err := mainCore(toActual(testOpts), []string{
		"auto-pr",
		`--branch`, `branch/{{ .Input "PR Name" }}`,
		`--commit`, `commit/{{ .Name }}`,
	})

	assert.ErrorContains(t, err, "value not supplied")
}

// newBuf is a small helper to create a bytes.Buffer pointer from a string,
// used when constructing GhExec return values.
func newBuf(s string) *bytes.Buffer {
	b := bytes.NewBufferString(s)
	return b
}
