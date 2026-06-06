package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/justindbaur/gh-codeowners/internal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func toActual(testOpts *internal.TestRootCmdOptions) *RootCmdOptions {
	return &RootCmdOptions{
		In:  testOpts.In,
		Out: testOpts.Out,
		Err: testOpts.Err,
		ReadFile: func(filePath string) (File, error) {
			args := testOpts.Mock.MethodCalled("ReadFile", filePath)
			return args.Get(0).(File), args.Error(1)
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

func TestPlanForFile_emptyPlanMultipleOwners(t *testing.T) {
	plan := NewPullRequestPlan()

	plan.PlanForFile("test-file.txt", []string{"@team-1", "@team-2"})

	// When there are multiple owners and nothing already in the team files list
	// it's expected to go to the first team, this could change if we offer a
	// smarter algorithm for spreading.
	assert.Equal(t, 1, len(plan.TeamFiles))
	assert.Equal(t, []string{"test-file.txt"}, plan.TeamFiles["@team-1"])
}

func TestPlanForFile_ownerAlreadyOwnsAFile(t *testing.T) {
	plan := NewPullRequestPlan()
	plan.TeamFiles["@team-2"] = []string{"somefile.txt"}

	plan.PlanForFile("test-file.txt", []string{"@team-1", "@team-2"})

	// When a team already owns a file the plan will always try to prefer that to minimize
	// the amount of PRs we create.
	assert.Equal(t, 1, len(plan.TeamFiles))
	assert.Equal(t, []string{"somefile.txt", "test-file.txt"}, plan.TeamFiles["@team-2"])
}

func TestPlanForFile_noOwners(t *testing.T) {
	plan := NewPullRequestPlan()

	plan.PlanForFile("test-file.txt", []string{})

	assert.Equal(t, []string{"test-file.txt"}, plan.UnownedFiles)
}

func TestPlanForFile_noOwnersExistingItemsInSlice(t *testing.T) {
	plan := NewPullRequestPlan()

	plan.UnownedFiles = []string{"somefile.txt"}

	plan.PlanForFile("test-file.txt", []string{})

	assert.Equal(t, []string{"somefile.txt", "test-file.txt"}, plan.UnownedFiles)
}

func TestMoveUnownedFiles_teamSelected(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", "Choose where to put 1 unowned files", "", []string{"@team-1", "@team-2", "Separate", "Choose for each"}).
		Return(1, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"test-file.txt"}

	plan.MoveUnownedFiles(toActual(opts))

	assert.Equal(t, []string{"two.txt", "test-file.txt"}, plan.TeamFiles["@team-2"])
}

func TestMoveUnownedFiles_separateSelected(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", "Choose where to put 1 unowned files", "", []string{"@team-1", "@team-2", "Separate", "Choose for each"}).
		Return(2, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"test-file.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))

	assert.NoError(t, err)
	assert.Equal(t, []string{"test-file.txt"}, plan.SeparateFiles)
}

func TestMoveUnownedFiles_chooseEachSelected(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", "Choose where to put 1 unowned files", "", []string{"@team-1", "@team-2", "Separate", "Choose for each"}).
		Return(3, nil)

	opts.Prompter.
		On("MultiSelect", mock.Anything, []string{}, []string{"@team-1", "@team-2", "Separate"}).
		Return([]int{0}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"test-file.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))

	assert.NoError(t, err)
	assert.Equal(t, []string{"one.txt", "test-file.txt"}, plan.TeamFiles["@team-1"])
}

func TestMoveUnownedFiles_chooseMultipleTeams(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", "Choose where to put 1 unowned files", "", []string{"@team-1", "@team-2", "Separate", "Choose for each"}).
		Return(3, nil)

	opts.Prompter.
		On("MultiSelect", mock.Anything, []string{}, []string{"@team-1", "@team-2", "Separate"}).
		Return([]int{0, 1}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"teo.txt"},
	}
	plan.UnownedFiles = []string{"test-file.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))

	assert.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"@team-1": {"test-file.txt"},
		"@team-2": {"test-file.txt"},
	}, plan.InteractiveStageFiles)
}

func TestMoveUnownedFiles_selectError(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", mock.Anything, mock.Anything, mock.Anything).
		Return(0, fmt.Errorf("terminal closed"))

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{"@team-1": {"one.txt"}}
	plan.UnownedFiles = []string{"unowned.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))
	assert.ErrorContains(t, err, "terminal closed")
}

func TestMoveUnownedFiles_chooseForEachNoItemsSelected(t *testing.T) {
	opts := internal.NewTestRootOpts()
	// "Choose for each" is the last general option (index 3 for 2 teams).
	opts.Prompter.
		On("Select", mock.Anything, mock.Anything, mock.Anything).
		Return(3, nil)
	opts.Prompter.
		On("MultiSelect", mock.Anything, mock.Anything, mock.Anything).
		Return([]int{}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"unowned.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))
	assert.ErrorContains(t, err, "must select at least one action")
}

func TestMoveUnownedFiles_chooseForEachSeparateWithTeamError(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", mock.Anything, mock.Anything, mock.Anything).
		Return(3, nil)
	// For 2 teams, specificOptions = ["@team-1", "@team-2", "Separate"].
	// Selecting both @team-1 (0) and Separate (2) together is invalid.
	opts.Prompter.
		On("MultiSelect", mock.Anything, mock.Anything, mock.Anything).
		Return([]int{0, 2}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"unowned.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))
	assert.ErrorContains(t, err, "cannot select Separate alongside a team")
}

func TestMoveUnownedFiles_chooseForEachSeparateOnly(t *testing.T) {
	// When the user selects only "Separate" in the per-file multi-select, the file
	// should land in SeparateFiles without any panic (tests the bug-fix guard on the
	// interactive-staging loop).
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", mock.Anything, mock.Anything, mock.Anything).
		Return(3, nil)
	// Index 2 = "Separate" (len(teamNames) == 2).
	opts.Prompter.
		On("MultiSelect", mock.Anything, mock.Anything, mock.Anything).
		Return([]int{2}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"unowned.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))
	assert.NoError(t, err)
	assert.Equal(t, []string{"unowned.txt"}, plan.SeparateFiles)
	assert.Empty(t, plan.InteractiveStageFiles)
}

func TestMoveUnownedFiles_chooseForEachUpdatesExistingTeamEntry(t *testing.T) {
	// Two unowned files both routed to @team-1 exercises the "found" branch of the
	// InteractiveStageFiles map update.
	opts := internal.NewTestRootOpts()
	opts.Prompter.
		On("Select", mock.Anything, mock.Anything, mock.Anything).
		Return(3, nil)
	opts.Prompter.
		On("MultiSelect", "What teams should 'first.txt' be put into (can select multiple)",
			[]string{}, []string{"@team-1", "@team-2", "Separate"}).
		Return([]int{0}, nil)
	opts.Prompter.
		On("MultiSelect", "What teams should 'second.txt' be put into (can select multiple)",
			[]string{}, []string{"@team-1", "@team-2", "Separate"}).
		Return([]int{0}, nil)

	plan := NewPullRequestPlan()
	plan.TeamFiles = map[string][]string{
		"@team-1": {"one.txt"},
		"@team-2": {"two.txt"},
	}
	plan.UnownedFiles = []string{"first.txt", "second.txt"}

	err := plan.MoveUnownedFiles(toActual(opts))
	assert.NoError(t, err)
	assert.Equal(t, []string{"first.txt", "second.txt"}, plan.InteractiveStageFiles["@team-1"])
}

func TestBuildShortNames_singleTeamPanics(t *testing.T) {
	assert.Panics(t, func() {
		buildShortNames([]string{"@only-one-team"})
	})
}

func newBodyTemplateTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("body", "", "")
	return cmd
}

func TestGetBodyTemplate_skipsWhenBodyFlagProvided(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	err := cmd.Flags().Set("body", "already set")
	assert.NoError(t, err)

	autoPrOpts := &AutoPROptions{BodyTemplate: "already set"}

	err = getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.NoError(t, err)
	assert.Equal(t, "already set", autoPrOpts.BodyTemplate)
	opts.Mock.AssertNotCalled(t, "GitExec", mock.Anything)
	opts.Mock.AssertNotCalled(t, "AskOne", mock.Anything, mock.Anything)
	opts.Prompter.AssertNotCalled(t, "Select", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetBodyTemplate_noTemplatesFound(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	tempDir := t.TempDir()

	opts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)

	autoPrOpts := &AutoPROptions{}

	err := getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.NoError(t, err)
	assert.Empty(t, autoPrOpts.BodyTemplate)
	opts.Mock.AssertNotCalled(t, "AskOne", mock.Anything, mock.Anything)
	opts.Prompter.AssertNotCalled(t, "Select", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetBodyTemplate_usesSelectedTemplateContents(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, ".github", "PULL_REQUEST_TEMPLATE.md")

	err := os.MkdirAll(filepath.Dir(templatePath), 0o755)
	assert.NoError(t, err)
	err = os.WriteFile(templatePath, []byte("Template from disk"), 0o644)
	assert.NoError(t, err)

	opts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	opts.Prompter.On("Select", "Choose a template", mock.Anything, []string{"PULL_REQUEST_TEMPLATE.md", "Start with a blank pull request"}).
		Return(0, nil)
	opts.Mock.On("ReadFile", templatePath).Return(&internal.TestFile{
		Contents: "Template from disk",
	}, nil)
	opts.Mock.On("AskOne", "Template from disk", mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "Final body"
	}).Return(nil)

	autoPrOpts := &AutoPROptions{}

	err = getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.NoError(t, err)
	assert.Equal(t, "Final body", autoPrOpts.BodyTemplate)
}

func TestGetBodyTemplate_blankOptionStartsEmpty(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, ".github", "PULL_REQUEST_TEMPLATE.md")

	err := os.MkdirAll(filepath.Dir(templatePath), 0o755)
	assert.NoError(t, err)
	err = os.WriteFile(templatePath, []byte("Template from disk"), 0o644)
	assert.NoError(t, err)

	opts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	opts.Prompter.On("Select", "Choose a template", mock.Anything, []string{"PULL_REQUEST_TEMPLATE.md", "Start with a blank pull request"}).
		Return(1, nil)
	opts.Mock.On("AskOne", "", mock.Anything).Run(func(args mock.Arguments) {
		contents := args.Get(1).(*string)
		*contents = "Body from scratch"
	}).Return(nil)

	autoPrOpts := &AutoPROptions{}

	err = getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.NoError(t, err)
	assert.Equal(t, "Body from scratch", autoPrOpts.BodyTemplate)
	opts.Mock.AssertNotCalled(t, "ReadFile", templatePath)
}

func TestGetBodyTemplate_readFileError(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, ".github", "PULL_REQUEST_TEMPLATE.md")

	err := os.MkdirAll(filepath.Dir(templatePath), 0o755)
	assert.NoError(t, err)
	err = os.WriteFile(templatePath, []byte("Template from disk"), 0o644)
	assert.NoError(t, err)

	opts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	opts.Prompter.On("Select", "Choose a template", mock.Anything, []string{"PULL_REQUEST_TEMPLATE.md", "Start with a blank pull request"}).
		Return(0, nil)
	opts.Mock.On("ReadFile", templatePath).Return(&internal.TestFile{}, fmt.Errorf("permission denied"))

	autoPrOpts := &AutoPROptions{}

	err = getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.ErrorContains(t, err, "permission denied")
}

func TestGetBodyTemplate_askOneError(t *testing.T) {
	opts := internal.NewTestRootOpts()
	cmd := newBodyTemplateTestCmd()
	tempDir := t.TempDir()
	templatePath := filepath.Join(tempDir, ".github", "PULL_REQUEST_TEMPLATE.md")

	err := os.MkdirAll(filepath.Dir(templatePath), 0o755)
	assert.NoError(t, err)
	err = os.WriteFile(templatePath, []byte("Template from disk"), 0o644)
	assert.NoError(t, err)

	opts.Mock.On("GitExec", []string{"rev-parse", "--show-toplevel"}).
		Return([]byte(tempDir+"\n"), nil)
	opts.Prompter.On("Select", "Choose a template", mock.Anything, []string{"PULL_REQUEST_TEMPLATE.md", "Start with a blank pull request"}).
		Return(0, nil)
	opts.Mock.On("ReadFile", templatePath).Return(&internal.TestFile{
		Contents: "Template from disk",
	}, nil)
	opts.Mock.On("AskOne", "Template from disk", mock.Anything).
		Return(fmt.Errorf("editor crashed"))

	autoPrOpts := &AutoPROptions{}

	err = getBodyTemplate(cmd, toActual(opts), autoPrOpts)

	assert.ErrorContains(t, err, "editor crashed")
}

func TestCreateStash_noChanges(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte{}, nil)

	stashRef, err := createStash(toActual(opts), "gh-codeowners:auto-pr:test")

	assert.NoError(t, err)
	assert.Empty(t, stashRef)
	opts.Mock.AssertNotCalled(t, "GitExec", []string{"stash", "push", "--include-untracked", "-m", "gh-codeowners:auto-pr:test"})
}

func TestCreateStash_returnsCreatedRef(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte("?? new-file.txt\n"), nil)
	opts.Mock.On("GitExec", []string{"stash", "push", "--include-untracked", "-m", "gh-codeowners:auto-pr:test"}).
		Return([]byte{}, nil)
	opts.Mock.On("GitExec", []string{"stash", "list", "--format=%gd\t%s"}).
		Return([]byte("stash@{1}\tother-message\nstash@{0}\tgh-codeowners:auto-pr:test\n"), nil)

	stashRef, err := createStash(toActual(opts), "gh-codeowners:auto-pr:test")

	assert.NoError(t, err)
	assert.Equal(t, "stash@{0}", stashRef)
}

func TestCreateStash_errorsWhenCreatedRefMissing(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Mock.On("GitExec", []string{"status", "--porcelain", "--untracked-files=all"}).
		Return([]byte("?? new-file.txt\n"), nil)
	opts.Mock.On("GitExec", []string{"stash", "push", "--include-untracked", "-m", "gh-codeowners:auto-pr:test"}).
		Return([]byte{}, nil)
	opts.Mock.On("GitExec", []string{"stash", "list", "--format=%gd\t%s"}).
		Return([]byte("stash@{0}\tother-message\n"), nil)

	stashRef, err := createStash(toActual(opts), "gh-codeowners:auto-pr:test")

	assert.Empty(t, stashRef)
	assert.ErrorContains(t, err, "could not find created stash")
}

func TestApplyStash_usesTargetedIndexRestore(t *testing.T) {
	opts := internal.NewTestRootOpts()
	opts.Mock.On("GitExec", []string{"stash", "pop", "--index", "stash@{2}"}).
		Return([]byte{}, nil)

	err := applyStash(toActual(opts), "stash@{2}")

	assert.NoError(t, err)
}
