package cmd

import (
	"bytes"
	"testing"

	"github.com/justindbaur/gh-codeowners/internal"
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
