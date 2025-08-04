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

type AutoPROptions struct {
	IsDraft        bool
	CommitTemplate string
	BranchTemplate string
	// TODO: Allow body in non-interactive way

	UnownedFiles string
	DryRun       bool
	Template     string
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

			filesMap := map[string][]string{}
			unownedFiles := []string{}

			for edittedFilesScanner.Scan() {
				owners := codeowners.FindOwners(edittedFilesScanner.Bytes())

				if len(owners) == 0 {
					unownedFiles = append(unownedFiles, edittedFilesScanner.Text())
					continue
				}

				// TODO: Could apply different algothims to spread load
				// For now attempt to minimize PR's by scanning if any of the owners already have an entry and if they do add it to the first one
				var foundEntry = false
				for _, owner := range owners {
					existingValue, found := filesMap[owner]
					if found {
						// Update
						foundEntry = true
						filesMap[owner] = append(existingValue, edittedFilesScanner.Text())
					}
				}

				if !foundEntry {
					// Insert it for the first owner
					filesMap[owners[0]] = []string{edittedFilesScanner.Text()}
				}
			}

			if len(unownedFiles) > 0 {
				// Let the user choose where to put unowned files
				options := append(slices.Collect(maps.Keys(filesMap)), "Separate")
				optionIndex, err := opts.Prompter.Select(fmt.Sprintf("Choose where to put %d unowned files", len(unownedFiles)), "", options)

				if err != nil {
					return fmt.Errorf("error requesting what to do with unowned files: %v", err)
				}

				option := options[optionIndex]

				existingValue, found := filesMap[option]

				if found {
					// Append
					filesMap[option] = append(existingValue, unownedFiles...)
				} else {
					// Insert
					filesMap[option] = unownedFiles
				}
			}

			if len(filesMap) == 0 {
				// Nothing to do, stop here
				return fmt.Errorf("there are no files to make PR's for")
			}

			if len(filesMap) == 1 {
				return fmt.Errorf("only one PR would be made, it's recommended to just use `gh pr create`")
			}

			branchTemplate, err := getBranchTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("problem getting branch template: %v", err)
			}

			commitTemplate, err := getCommitTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("error getting commit template: %v", err)
			}

			bodyTemplate, err := getBodyTemplate(cmd, opts, autoPROpts)

			if err != nil {
				return fmt.Errorf("error getting body template: %v", err)
			}

			remoteName, err := opts.GetRemoteName()

			if err != nil {
				return fmt.Errorf("could not determine remote name: %v", err)
			}

			shortNames := buildShortNames(slices.Collect(maps.Keys(filesMap)))

			var number = 1

			// Track checked out branches so we can help "unique-ify" it for them
			checkedOutBranches := []string{}

			// TODO: Do this loop with some sort that makes it do it the same way each time
			for team, files := range filesMap {
				templateData := &TemplateData{
					Number:     number,
					TeamId:     team,
					Name:       shortNames[team],
					Files:      files,
					Promote:    promotionString,
					prompter:   opts.Prompter,
					inputCache: map[string]string{},
				}
				number++

				cmd.Printf("Creating PR for team: %s\n", team)

				teamBranch, err := executeToString(branchTemplate, templateData)

				if err != nil {
					return fmt.Errorf("error while formatting branch template: %v", err)
				}

				if slices.Contains(checkedOutBranches, teamBranch) {
					cmd.Printf("Branch '%s' is not unique, adding incrementing number to the branch name\n", teamBranch)
					// Their branch name is NOT unique, re-execute with the number thrown onto the end
					// Technically... this branch could not be unique still but I think they are just messing with me if that's
					// the case.
					teamBranch = fmt.Sprintf("%s-%d", teamBranch, templateData.Number)
				}

				// Track branch
				checkedOutBranches = append(checkedOutBranches, teamBranch)

				checkoutArgs := []string{"checkout", "-b", teamBranch}

				// Checkout
				checkoutOutput, err := opts.GitExec(checkoutArgs...)

				if err != nil {
					// Possible errors:
					// 1. Branch already exists
					cmd.Println("Error doing git checkout operation")
					cmd.Println(err)
					os.Stdout.Write(checkoutOutput)
					return fmt.Errorf("error checking out branch '%s': %v", teamBranch, err)
				}

				// Stage files for this team
				addOutput, err := opts.GitExec(append([]string{"add"}, files...)...)

				if err != nil {
					// Possible errors:
					// 1.
					cmd.Println("Error doing git add operation")
					cmd.Println(err)
					os.Stdout.Write(addOutput)
					return fmt.Errorf("problem adding files")
				}

				teamCommit, err := executeToString(commitTemplate, templateData)

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
					cmd.Println(err)
					os.Stderr.Write(commitOutput)
					return fmt.Errorf("problem committing code for team '%s': %v", team, err)
				}

				cmd.Println("git commit")
				os.Stdout.Write(commitOutput)

				// Push branch
				pushArgs := []string{"push", "--set-upstream", remoteName, teamBranch}

				pushOutput, err := opts.GitExec(pushArgs...)

				if err != nil {
					// Possible errors:
					// 1. Branch already exists in the remote
					cmd.Println("Error doing git push operation")
					cmd.Println(err)
					os.Stderr.Write(pushOutput)
					// TODO: Return actual error
					return nil
				}

				// TODO: We could make this safer and remove unsafe path characters
				file, err := os.CreateTemp(os.TempDir(), "team_pr_body")

				if err != nil {
					cmd.Println(err)
					// TODO: Return actual error
					return nil
				}

				defer file.Close()
				defer os.Remove(file.Name())

				teamBody, err := executeToString(bodyTemplate, templateData)

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
					fmt.Sprintf("--draft=%t", autoPROpts.IsDraft),
					fmt.Sprintf("--dry-run=%t", autoPROpts.DryRun),
				}

				cmd.Println(args)

				stdOut, stdErr, err := opts.GhExec(args...)

				if err != nil {
					return fmt.Errorf("error creating PR with GitHub CLI: %v", err)
				}

				// Should we do anything else here?
				os.Stdout.Write(stdOut.Bytes())
				os.Stderr.Write(stdErr.Bytes())

				// Checkout back to the last branch so we can continue with new teams or leave the user back where they were
				checkoutOutput, err = opts.GitExec("checkout", "-")

				if err != nil {
					return fmt.Errorf("error trying to checkout base branch: %v", err)
				}

				os.Stdout.Write(checkoutOutput)
				cmd.Printf("Finished making PR for %s\n", team)
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
	fl.StringVarP(&autoPROpts.Template, "template", "T", "", "The template `file` to use when creating the templated team PR")

	_ = cmd.RegisterFlagCompletionFunc("unowned-files", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// TODO: Do early parse of CODEOWNERS file to help fill in option
		return nil, cobra.ShellCompDirectiveError
	})

	return cmd
}

func getBranchTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) (*template.Template, error) {
	var templateString = ""

	if cmd.Flags().Changed("branch") {
		templateString = autoPrOpts.BranchTemplate
	} else {
		var err error
		templateString, err = rootOpts.Prompter.Input("What branch template do you want?", "")

		if err != nil {
			return nil, err
		}

		if templateString == "" {
			return nil, fmt.Errorf("branch template is required")
		}
	}

	parsedTemplate, err := template.New("Branch Template").Parse(templateString)

	if err != nil {
		return nil, err
	}

	return parsedTemplate, nil
}

func getCommitTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) (*template.Template, error) {
	var templateString = ""

	if cmd.Flags().Changed("commit") {
		templateString = autoPrOpts.CommitTemplate
	} else {
		var err error
		templateString, err = rootOpts.Prompter.Input("What commit/PR title template do you want?", "Files for {{ .Team }}")

		if err != nil {
			return nil, err
		}

		if templateString == "" {
			return nil, fmt.Errorf("commit template is required")
		}
	}

	parsedTemplate, err := template.New("Commit Template").Parse(templateString)

	if err != nil {
		return nil, err
	}

	return parsedTemplate, nil
}

func getBodyTemplate(cmd *cobra.Command, rootOpts *RootCmdOptions, autoPrOpts *AutoPROptions) (*template.Template, error) {
	var initialPrContents = ""

	if autoPrOpts.Template == "" {
		topLevelDirBytes, err := rootOpts.GitExec("rev-parse", "--show-toplevel")

		if err != nil {
			return nil, fmt.Errorf("could not find top level dir: %v", err)
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
				return nil, fmt.Errorf("could not get PR template")
			}

			// Is this the last option that we insert for blank
			if len(templates) != templateOption {
				templateFile, err := rootOpts.ReadFile(templates[templateOption])

				if err != nil {
					return nil, fmt.Errorf("problem opening selected PR template: %v", err)
				}

				defer templateFile.Close()

				builder := new(strings.Builder)
				_, err = io.Copy(builder, templateFile.Reader)

				if err != nil {
					return nil, fmt.Errorf("error while reading PR template file contents: %v", err)
				}

				initialPrContents = builder.String()
			}
		}
	} else {
		templateFile, err := rootOpts.ReadFile(autoPrOpts.Template)

		if err != nil {
			return nil, fmt.Errorf("could not open the given template file '%s': %v", autoPrOpts.Template, err)
		}

		defer templateFile.Close()

		builder := new(strings.Builder)
		_, err = io.Copy(builder, templateFile.Reader)

		if err != nil {
			return nil, fmt.Errorf("error while reading PR template file contents: %v", err)
		}

		initialPrContents = builder.String()
	}

	var contents = ""
	err := rootOpts.AskOne(initialPrContents, &contents)

	if err != nil {
		return nil, fmt.Errorf("error while requesting PR template edit: %v", err)
	}

	bodyTemplate, err := template.New("Body Template").Parse(contents)

	if err != nil {
		return nil, fmt.Errorf("parsing body template: %v", err)
	}

	return bodyTemplate, nil
}

func executeToString(template *template.Template, data *TemplateData) (string, error) {
	buf := new(bytes.Buffer)
	err := template.Execute(buf, data)
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
