package cmd

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/cli/cli/v2/pkg/githubtemplate"
	"github.com/spf13/cobra"
)

type PullRequestPlan struct {
	// A map of team name to the files that should go into their PR
	TeamFiles map[string][]string
	// Files that don't have an owner
	UnownedFiles []string
	// A slice of files that should be put into their own PR
	SeparateFiles []string
	// A map of team name to files that should go through interactive staging
	InteractiveStageFiles map[string][]string
}

func (plan *PullRequestPlan) PlanForFile(file string, owners []string) {
	if len(owners) == 0 {
		plan.UnownedFiles = append(plan.UnownedFiles, file)
		return
	}

	// TODO: Could apply different algothims to spread load
	// For now attempt to minimize PR's by scanning if any of the owners already have an entry and if they do add it to the first one
	var foundEntry = false
	for _, owner := range owners {
		existingValue, found := plan.TeamFiles[owner]
		if found {
			// Update
			foundEntry = true
			plan.TeamFiles[owner] = append(existingValue, file)
		}
	}

	if !foundEntry {
		// Insert it for the first owner
		plan.TeamFiles[owners[0]] = []string{file}
	}
}

func (plan *PullRequestPlan) HasUnownedFiles() bool {
	return len(plan.UnownedFiles) > 0
}

func (plan *PullRequestPlan) MoveUnownedFiles(opts *RootCmdOptions) error {
	teamNames := slices.Collect(maps.Keys(plan.TeamFiles))
	generalOptions := append(teamNames, "Separate", "Choose for each")
	generalOptionIndex, err := opts.Prompter.Select(fmt.Sprintf("Choose where to put %d unowned files", len(plan.UnownedFiles)), "", generalOptions)

	if err != nil {
		return err
	}

	generalOption := generalOptions[generalOptionIndex]

	// TODO: Do we need to handle empty string?

	switch generalOption {
	case "Choose for each":
		// TODO: Could allow them to view the file and if they choose that option show the options again after showing a diff
		specificOptions := append(teamNames, "Separate")

		for _, unownedFile := range plan.UnownedFiles {
			// Prompt for each file
			selectedOptions, err := opts.Prompter.MultiSelect(fmt.Sprintf("What teams should '%s' be put into (can select multiple)", unownedFile), []string{}, specificOptions)

			if err != nil {
				return err
			}

			numberOfOptionsSelected := len(selectedOptions)

			if numberOfOptionsSelected == 0 {
				return fmt.Errorf("must select at least one action")
			}

			// Did they select a team AND separate?
			if numberOfOptionsSelected > 1 && slices.Contains(selectedOptions, len(teamNames)) {
				return fmt.Errorf("cannot select Separate alongside a team")
			}

			if numberOfOptionsSelected == 1 {
				// Did they select Separate
				if selectedOptions[0] == len(teamNames) {
					plan.SeparateFiles = append(plan.SeparateFiles, unownedFile)
				} else {
					// Add to the selected team
					selectedTeam := teamNames[selectedOptions[0]]
					plan.TeamFiles[selectedTeam] = append(plan.TeamFiles[selectedTeam], unownedFile)
				}
			}

			for _, selectedIndex := range selectedOptions {
				// All these should be a team name
				selectedTeam := teamNames[selectedIndex]

				existingValue, found := plan.InteractiveStageFiles[selectedTeam]
				if found {
					plan.InteractiveStageFiles[selectedTeam] = append(existingValue, unownedFile)
				} else {
					plan.InteractiveStageFiles[selectedTeam] = []string{unownedFile}
				}
			}
		}
	case "Separate":
		// Copy unowned files to the seperate files list
		plan.SeparateFiles = plan.UnownedFiles
	default:
		// An entry for this HAS to exist in the map, so append to it
		plan.TeamFiles[generalOption] = append(plan.TeamFiles[generalOption], plan.UnownedFiles...)
	}

	return nil
}

func (plan *PullRequestPlan) DoInteractiveStagingIfNeeded(team string, opts *RootCmdOptions) error {
	filesToStage, found := plan.InteractiveStageFiles[team]

	if !found {
		// No files need to be interactively staged for this team, skip
		return nil
	}

	return opts.GitExecInt(append([]string{"add", "--interactive"}, filesToStage...)...)
}

func NewPullRequestPlan() *PullRequestPlan {
	return &PullRequestPlan{
		TeamFiles:             map[string][]string{},
		UnownedFiles:          []string{},
		SeparateFiles:         []string{},
		InteractiveStageFiles: map[string][]string{},
	}
}

type AutoPROptions struct {
	IsDraft        bool
	CommitTemplate string
	BranchTemplate string
	RemoteName     string
	BodyTemplate   string
	UnownedFiles   string
	DryRun         bool
}

func newCmdAutoPR(opts *RootCmdOptions) *cobra.Command {
	autoPROpts := &AutoPROptions{}

	cmd := &cobra.Command{
		Use:     "auto-pr",
		Aliases: []string{"pr"},
		Short:   "Make many PR's from one changeset",
		Long: `The commit, branch, and PR template file are all allowed to use a go template strings. Branches are required to
use a template string that will result in a unique name amongst all teams if one is not given, this will append a incrementing
number to the branch name. Template strings make use of go text/template using the '{{ .TeamId }}' syntax. In addtion to 'TeamId'
you may use 'Number' which is an incrementing number for the number of PR's being created, 'Name' which is the team name with 
common prefixes and suffixes removed, 'Files' which is a slice of the files being added to this PR, 'Promote' is replaced with
a link to this tool. You can also invoke the '{{ .Input "my_value" }} function. This lets you prompt yourself for a value for
each team.'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			edittedFilesScanner, err := GetEdittedFilesScanner(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting editted files scanner: %v", err)
			}

			codeowners, err := GetCodeowners(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting codeowners info: %v", err)
			}

			prPlan := NewPullRequestPlan()

			for edittedFilesScanner.Scan() {
				owners := codeowners.FindOwners(edittedFilesScanner.Bytes())
				prPlan.PlanForFile(edittedFilesScanner.Text(), owners)
			}

			if prPlan.HasUnownedFiles() {
				err = prPlan.MoveUnownedFiles(opts)

				if err != nil {
					return fmt.Errorf("issue moving unowned files: %v", err)
				}
			}

			if len(prPlan.TeamFiles) == 0 {
				// Nothing to do, stop here
				return fmt.Errorf("there are no files to make PR's for")
			}

			if len(prPlan.TeamFiles) == 1 {
				return fmt.Errorf("only one PR would be made, it's recommended to just use `gh pr create`")
			}

			err = getBranchTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("problem getting branch template: %v", err)
			}

			err = getCommitTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("error getting commit template: %v", err)
			}

			err = getBodyTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("error getting body template: %v", err)
			}

			if !cmd.Flags().Changed("remote") {
				remoteName, err := opts.GetRemoteName()

				if err != nil {
					return fmt.Errorf("could not determine remote name: %v", err)
				}

				autoPROpts.RemoteName = remoteName
			}

			// TODO: Possibly remove "Separate" from the PR's to make short names from
			shortNames := buildShortNames(slices.Collect(maps.Keys(prPlan.TeamFiles)))

			var number = 1

			// Track checked out branches so we can help "unique-ify" it for them
			checkedOutBranches := []string{}

			// TODO: Do this loop with some sort that makes it do it the same way each time
			for team, files := range prPlan.TeamFiles {
				err = createPullRequest(cmd, &checkedOutBranches, autoPROpts, opts, &TemplateData{
					Number:     number,
					TeamId:     team,
					Name:       shortNames[team],
					Files:      files,
					Promote:    promotionString,
					prompter:   opts.Prompter,
					inputCache: map[string]string{},
				}, func(t string) error {
					return prPlan.DoInteractiveStagingIfNeeded(t, opts)
				})

				if err != nil {
					return fmt.Errorf("issue creating PR for team %s: %v", team, err)
				}

				number++
			}

			if len(prPlan.SeparateFiles) > 0 {
				err = createPullRequest(cmd, &checkedOutBranches, autoPROpts, opts, &TemplateData{
					Number:     number,
					Name:       "Separate",
					TeamId:     "Separate Pull Request",
					Files:      prPlan.SeparateFiles,
					prompter:   opts.Prompter,
					inputCache: map[string]string{},
					Promote:    promotionString,
				}, func(team string) error {
					// Separate PR will never do interactive staging
					return nil
				})

				if err != nil {
					return fmt.Errorf("issue creating separate PR: %v", err)
				}
			}

			return nil
		},
	}

	fl := cmd.Flags()
	fl.StringVarP(&autoPROpts.CommitTemplate, "commit", "c", "", "The template string to use for each commit")
	fl.StringVarP(&autoPROpts.BranchTemplate, "branch", "b", "", "The template string to use for each branch that is created")
	fl.StringVarP(&autoPROpts.UnownedFiles, "unowned-files", "u", "", "What PR to put unowned files onto. `separate` to make their own PR.")
	fl.BoolVarP(&autoPROpts.IsDraft, "draft", "d", false, "Mark the pull requests as drafts")
	fl.BoolVar(&autoPROpts.DryRun, "dry-run", false, "Print details instead of creating the PR. May still push git changes.")
	fl.StringVar(&autoPROpts.BodyTemplate, "body", "", "The template `file` to use when creating the templated team PR")
	fl.StringVarP(&autoPROpts.RemoteName, "remote", "r", "", "The remote the PR will be created on")

	_ = cmd.RegisterFlagCompletionFunc("unowned-files", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// TODO: Do early parse of CODEOWNERS file to help fill in option
		return nil, cobra.ShellCompDirectiveError
	})

	return cmd
}

func createPullRequest(cmd *cobra.Command, checkedOutBranches *[]string, prOpts *AutoPROptions, opts *RootCmdOptions, data *TemplateData, runInteractiveStaging func(team string) error) error {
	cmd.Printf("Creating PR for team: %s\n", data.TeamId)

	teamBranch, err := executeToString("Branch Template", prOpts.BranchTemplate, data)

	if err != nil {
		return fmt.Errorf("error while formatting branch template: %v", err)
	}

	if slices.Contains(*checkedOutBranches, teamBranch) {
		cmd.Printf("Branch '%s' is not unique, adding incrementing number to the branch name\n", teamBranch)
		// Their branch name is NOT unique, re-execute with the number thrown onto the end
		// Technically... this branch could not be unique still but I think they are just messing with me if that's
		// the case.
		teamBranch = fmt.Sprintf("%s-%d", teamBranch, data.Number)
	}

	// Track branch
	*checkedOutBranches = append(*checkedOutBranches, teamBranch)

	checkoutArgs := []string{"checkout", "-b", teamBranch}

	// Checkout
	checkoutOutput, err := opts.GitExec(checkoutArgs...)

	if err != nil {
		// Possible errors:
		// 1. fatal: a branch named 'test' already exists
		//   exit code: 128
		cmd.Println("Error doing git checkout operation")
		cmd.ErrOrStderr().Write(checkoutOutput)
		return fmt.Errorf("error checking out branch '%s': %v", teamBranch, err)
	}

	// Stage files for this team
	addOutput, err := opts.GitExec(append([]string{"add"}, data.Files...)...)

	if err != nil {
		// Possible errors:
		// 1.
		cmd.Println("Error doing git add operation")
		cmd.ErrOrStderr().Write(addOutput)
		return fmt.Errorf("problem adding files: %v", err)
	}

	// Possibly do interactive staging
	err = runInteractiveStaging(data.TeamId)

	if err != nil {
		return fmt.Errorf("issue doing interactive staging: %v", err)
	}

	teamCommit, err := executeToString("Commit Template", prOpts.CommitTemplate, data)

	if err != nil {
		return fmt.Errorf("error while getting commit message template: %v", err)
	}

	// Create commit
	commitArgs := []string{"commit", "--message", teamCommit}

	commitOutput, err := opts.GitExec(commitArgs...)

	if err != nil {
		// Possible errors:
		// 1.
		cmd.Println("Error doing git commit operation")
		cmd.ErrOrStderr().Write(commitOutput)
		return fmt.Errorf("problem committing code for team '%s': %v", data.TeamId, err)
	}

	// Push branch
	pushArgs := []string{"push", "--set-upstream", prOpts.RemoteName, teamBranch}

	pushOutput, err := opts.GitExec(pushArgs...)

	if err != nil {
		// Possible errors:
		// 1. Branch already exists in the remote
		cmd.Printf("Error doing git push operation: %v\n", pushArgs)
		cmd.Println(err)
		cmd.ErrOrStderr().Write(pushOutput)
		return fmt.Errorf("problem pushing to remote")
	}

	file, err := os.CreateTemp(os.TempDir(), "team_pr_body")

	if err != nil {
		return fmt.Errorf("problem creating temp dir: %v", err)
	}

	defer file.Close()
	defer os.Remove(file.Name())

	teamBody, err := executeToString("Body Template", prOpts.BodyTemplate, data)

	if err != nil {
		return fmt.Errorf("error while formatting PR body: %v", err)
	}

	_, err = file.WriteString(teamBody)

	if err != nil {
		return fmt.Errorf("problem writing PR body file: %v", err)
	}

	// TODO: We can support more args from `gh pr create`
	args := []string{
		"pr",
		"new",
		"--body-file",
		file.Name(),
		"--title",
		teamCommit,
		fmt.Sprintf("--draft=%t", prOpts.IsDraft),
		fmt.Sprintf("--dry-run=%t", prOpts.DryRun),
	}

	stdOut, stdErr, err := opts.GhExec(args...)

	if err != nil {
		cmd.Printf("Problem creating PR with gh CLI: %v\n", args)
		cmd.OutOrStdout().Write(stdOut.Bytes())
		cmd.ErrOrStderr().Write(stdErr.Bytes())
		return fmt.Errorf("error creating PR with GitHub CLI: %v", err)
	}

	// Stdout should be a url to the PR
	cmd.Printf("PR for %s: %s", data.TeamId, stdOut.String())

	err = createStash(opts)

	if err != nil {
		cmd.Println("Failed to create stash")
		return err
	}

	// TODO: This doesn't work if interactive staging happened
	// Checkout back to the last branch so we can continue with new teams or leave the user back where they were
	checkoutOutput, err = opts.GitExec("checkout", "-")

	if err != nil {
		cmd.Println("Could not checkout last branch")
		cmd.ErrOrStderr().Write(checkoutOutput)
		return fmt.Errorf("error trying to checkout base branch: %v", err)
	}

	err = applyStash(opts)

	if err != nil {
		cmd.Println("Failed to apply stash")
		return err
	}

	return nil
}

func getBranchTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) error {
	if !cmd.Flags().Changed("branch") {
		var err error
		templateString, err := rootOpts.Prompter.Input("What branch template do you want?", "")

		if err != nil {
			return err
		}

		if templateString == "" {
			return fmt.Errorf("branch template is required")
		}

		autoPrOpts.BranchTemplate = templateString
	}

	return nil
}

func getCommitTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) error {
	if !cmd.Flags().Changed("commit") {
		templateString, err := rootOpts.Prompter.Input("What commit/PR title template do you want?", "Files for {{ .TeamId }}")

		if err != nil {
			return err
		}

		if templateString == "" {
			return fmt.Errorf("commit template is required")
		}

		autoPrOpts.CommitTemplate = templateString
	}

	return nil
}

func getBodyTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) error {
	if !cmd.Flags().Changed("body") {
		// Get body and place on autoPrOpts
		topLevelDirBytes, err := rootOpts.GitExec("rev-parse", "--show-toplevel")

		if err != nil {
			return err
		}

		topLevelDir := strings.Trim(string(topLevelDirBytes), "\n")

		const filePattern = "PULL_REQUEST_TEMPLATE"

		// Get the top level dir another way
		templates := githubtemplate.FindNonLegacy(topLevelDir, filePattern)

		if legacyTemplate := githubtemplate.FindLegacy(topLevelDir, filePattern); legacyTemplate != "" {
			templates = append(templates, legacyTemplate)
		}

		if len(templates) > 0 {
			templateNames := make([]string, len(templates))

			for i, templatePath := range templates {
				templateNames[i] = githubtemplate.ExtractName(templatePath)
			}

			templateOption, err := rootOpts.Prompter.Select("Choose a template", templates[0], append(templateNames, "Start with a blank pull request"))
			if err != nil {
				return err
			}

			var initialContents = ""

			// Is this the last option that we insert for blank
			if len(templates) != templateOption {
				templateFile, err := rootOpts.ReadFile(templates[templateOption])

				if err != nil {
					return err
				}

				defer templateFile.Close()

				builder := new(strings.Builder)
				_, err = io.Copy(builder, templateFile.Reader())

				if err != nil {
					return err
				}

				initialContents = builder.String()
			}

			var contents string
			err = rootOpts.AskOne(initialContents, &contents)

			if err != nil {
				return err
			}

			autoPrOpts.BodyTemplate = contents
		}
	}

	return nil
}

func executeToString(name, templateText string, data *TemplateData) (string, error) {
	t, err := template.New(fmt.Sprintf("%s: %s", name, data.Name)).Parse(templateText)

	if err != nil {
		return "", fmt.Errorf("failed to parse %s template for team %s", name, data.Name)
	}

	buf := new(bytes.Buffer)
	err = t.Execute(buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func findPrefixLength(values []string) int {
	var longestPrefix = 0
	var endPrefix = false

	if len(values) > 0 {
		sort.Strings(values)
		first := values[0]
		last := values[len(values)-1]

		for i := 0; i < len(first); i++ {
			if !endPrefix && last[i] == first[i] {
				longestPrefix++
			} else {
				endPrefix = true
			}
		}
	}

	return longestPrefix
}

func reverse(s string) string {
	n := len(s)
	runes := make([]rune, n)
	for _, rune := range s {
		n--
		runes[n] = rune
	}

	return string(runes[n:])
}

func reverseStrings(values []string) []string {
	output := make([]string, len(values))
	for i, s := range values {
		output[i] = reverse(s)
	}

	return output
}

func buildShortNames(teams []string) map[string]string {
	if len(teams) < 2 {
		panic("buildShortNames should not be called with fewer than 2 names")
	}

	prefixLength := findPrefixLength(teams)
	suffixLength := findPrefixLength(reverseStrings(teams))

	output := map[string]string{}
	for _, team := range teams {
		if suffixLength == 0 {
			output[team] = team[prefixLength:]
		} else {
			output[team] = team[prefixLength : len(team)-suffixLength]
		}
	}

	return output
}

func createStash(opts *RootCmdOptions) error {
	// TODO: Make smarter with a custom message
	_, err := opts.GitExec("stash", "push")
	return err
}

func applyStash(opts *RootCmdOptions) error {
	// TODO: Make smarter and apply the stash this program creates by name
	_, err := opts.GitExec("stash", "pop")
	return err
}

type TemplateData struct {
	Number     int
	Name       string
	TeamId     string
	Files      []string
	Promote    string
	prompter   Prompter
	inputCache map[string]string
}

const promotionString = "Made by [`gh-codeowners`](https://github.com/justindbaur/gh-codeowners)"

func (d *TemplateData) Input(name string) (string, error) {
	existingValue, found := d.inputCache[name]

	if !found {
		val, err := d.prompter.Input(fmt.Sprintf("%s: %s", d.Name, name), "")

		if err != nil {
			return "", fmt.Errorf("problem while prompting team '%s' for name '%s'", d.Name, name)
		}

		if val == "" {
			return "", fmt.Errorf("value not supplied for name '%s' for team '%s'", name, d.Name)
		}

		d.inputCache[name] = val
		return val, nil
	}

	return existingValue, nil
}
