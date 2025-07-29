package cmd

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

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

			// Avoid prompting if they supplied it via a flag
			var branchTemplate string
			if cmd.Flags().Changed("branch") {
				// They did supply a branch
				branchTemplate = autoPROpts.BranchTemplate
			} else {
				var err error
				branchTemplate, err = opts.Prompter.Input("Enter branch template", "")

				if err != nil {
					return fmt.Errorf("error while requesting branch template: %v", err)
				}
			}

			var initialPrContents = ""

			if autoPROpts.Template == "" {
				topLevelDirBytes, err := opts.GitExec("rev-parse", "--show-toplevel")

				if err != nil {
					return fmt.Errorf("could not find top level dir: %v", err)
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

					templateOption, err := opts.Prompter.Select("Choose a template", templates[0], append(templateNames, "Start with a blank pull request"))
					if err != nil {
						return fmt.Errorf("could not get PR template")
					}

					// Is this the last option that we insert for blank
					if len(templates) != templateOption {
						templateFile, err := opts.ReadFile(templates[templateOption])

						if err != nil {
							return fmt.Errorf("problem opening selected PR template: %v", err)
						}

						defer templateFile.Close()

						builder := new(strings.Builder)
						_, err = io.Copy(builder, templateFile.Reader)

						if err != nil {
							return fmt.Errorf("error while reading PR template file contents: %v", err)
						}

						initialPrContents = builder.String()
					}
				}
			} else {
				templateFile, err := opts.ReadFile(autoPROpts.Template)

				if err != nil {
					return fmt.Errorf("could not open the given template file '%s': %v", autoPROpts.Template, err)
				}

				defer templateFile.Close()

				builder := new(strings.Builder)
				_, err = io.Copy(builder, templateFile.Reader)

				if err != nil {
					return fmt.Errorf("error while reading PR template file contents: %v", err)
				}

				initialPrContents = builder.String()
			}

			var commitMessageTemplate string
			if cmd.Flags().Changed("commit") {
				commitMessageTemplate = autoPROpts.CommitTemplate
			} else {
				var err error
				commitMessageTemplate, err = opts.Prompter.Input("Enter the commit message you want to use", "")

				if err != nil {
					return fmt.Errorf("error while getting commit message template: %v", err)
				}
			}

			var contents = ""
			err = opts.AskOne(initialPrContents, &contents)

			if err != nil {
				return fmt.Errorf("error while requesting PR template edit: %v", err)
			}

			cmd.Println("Finished prompting the user.")

			cmd.Printf("Creating PR's for %d teams\n", len(filesMap))

			// TODO: Do loop over teams
			for team, files := range filesMap {

				fmtOptions := &FormatOptions{
					TeamSlug:          team,
					NumberOfFiles:     len(files),
					Files:             strings.Join(files, "\n"),
					TemplateVariables: map[string]string{},
				}

				cmd.Printf("Creating PR for team: %s\n", team)

				teamBranch, err := formatString(opts.Prompter, branchTemplate, fmtOptions)

				if err != nil {
					return fmt.Errorf("error while formatting branch template: %v", err)
				}

				checkoutArgs := []string{"checkout", "-b", teamBranch}

				// Checkout
				checkoutOutput, err := opts.GitExec(checkoutArgs...)

				if err != nil {
					cmd.Println("Error doing git checkout operation")
					cmd.Println(err)
					os.Stdout.Write(checkoutOutput)
					// TODO: Return actual error
					return nil
				}

				cmd.Println("git checkout -b output")
				os.Stdout.Write(checkoutOutput)

				// Stage files for this team
				addOutput, err := opts.GitExec(append([]string{"add"}, files...)...)

				if err != nil {
					cmd.Println("Error doing git add operation")
					cmd.Println(err)
					os.Stdout.Write(addOutput)
					// TODO: Return actual error
					return nil
				}

				cmd.Println("git add")
				os.Stdout.Write(addOutput)

				teamCommitMessage, err := formatString(opts.Prompter, commitMessageTemplate, fmtOptions)

				if err != nil {
					return fmt.Errorf("error while formatting commit message template: %v", err)
				}

				// Create commit
				commitArgs := []string{"commit", "--message", teamCommitMessage}

				commitOutput, err := opts.GitExec(commitArgs...)

				if err != nil {
					cmd.Println("Error doing git commit operation")
					cmd.Println(err)
					os.Stderr.Write(commitOutput)
					// TODO: Return actual error
					return nil
				}

				cmd.Println("git commit")
				os.Stdout.Write(commitOutput)

				// Push branch
				// TODO: origin might not be their remote name, we might need to give them an option
				pushArgs := []string{"push", "--set-upstream", "origin", teamBranch}

				// TODO: Currently if the branch exists on remote this fails, show better error or avoid the error in the first place?
				pushOutput, err := opts.GitExec(pushArgs...)

				if err != nil {
					cmd.Println("Error doing git push operation")
					cmd.Println(err)
					os.Stderr.Write(pushOutput)
					// TODO: Return actual error
					return nil
				}

				cmd.Println("git push")
				os.Stdout.Write(pushOutput)

				// TODO: We could make this safer and remove unsafe path characters
				file, err := os.CreateTemp(os.TempDir(), "team_pr_body")

				if err != nil {
					cmd.Println(err)
					// TODO: Return actual error
					return nil
				}

				defer file.Close()
				defer os.Remove(file.Name())

				newString, err := formatString(opts.Prompter, contents, fmtOptions)

				if err != nil {
					return fmt.Errorf("error while formatting PR body: %v", err)
				}

				_, err = file.WriteString(newString)

				if err != nil {
					cmd.Println(err)
					// TODO: Return actual error
					return nil
				}

				// TODO: Take option for allowing it to create drafts or not
				args := []string{
					"pr",
					"new",
					"--body-file",
					file.Name(),
					"--title",
					teamCommitMessage,
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

	cmd.SetHelpTemplate(
		`Interactively create a PR for a single team based on the files in the current working tree.

USAGE:
    gh codeowners auto-pr [flags]

ALIASES:
    pr

FLAGS:
    -c, --commit           The template string to use for the commit message for each team
    -b, --branch           The template string to use for the branch for each team
    -u, --unowned-files    The team or 'separate' to configure which PR to put unowned files into
    -d, --draft            Whether or not to mark the pull requests as drafts
    --dry-run              Print details instead of creating the PR. May still push git changes

INHERITED FLAGS:
    --help                 Show help for command

EXAMPLES:
    $ gh codeowners auto-pr
    $ gh codeowners auto-pr --commit "Do work for {Team Name}" --branch "feature/do-{Team Name}-work" --unowned-files separate --draft
    $ gh codeowners auto-pr --dry-run

LEARN MORE:
    The commit, branch, and PR template file are all allowed to use a template strings. Branches are required to
    use a template string that will result in a unique name amongst all teams. Template strings make use of
    template holes denoted by '{}', there are three reserved template holes '{slug}', '{numberOfFiles}', and '{files}'
    the 'slug' is the team name as shown in the CODEOWNERS file. This slug is often not very branch path safe which
    is why we recommend using a custom template hole. A custom template hole is any text surrounded by curly braces.
    this means you can put '{My Custom Template Hole}' and will get automatically prompted to enter that value for
    each team you are making a PR for.
`)

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

type FormatOptions struct {
	TeamSlug          string
	NumberOfFiles     int
	Files             string
	TemplateVariables map[string]string
}

func formatString(p Prompter, input string, options *FormatOptions) (output string, err error) {
	// Do all reserved words first
	output = strings.ReplaceAll(input, "{slug}", options.TeamSlug)
	output = strings.ReplaceAll(output, "{numberOfFiles}", strconv.Itoa(options.NumberOfFiles))
	output = strings.ReplaceAll(output, "{files}", options.Files)

	// TODO: Can probably write this loop better
	curIndex := 0
	for {
		if curIndex > len(output) {
			break
		}

		partial := output[curIndex:]
		braceIndex := strings.Index(partial, "{")

		if braceIndex == -1 {
			// Nothing more to format
			break
		}

		endIndex := strings.Index(partial[braceIndex+1:], "}")

		if endIndex == -1 {
			// Not a valid formatted string, ignore it
			curIndex = braceIndex
			continue
		}

		innerText := partial[braceIndex+1 : braceIndex+1+endIndex]

		existingValue, found := options.TemplateVariables[innerText]

		if !found {
			// We haven't seen this value yet, prompt for it and cache it
			existingValue, err = p.Input(fmt.Sprintf("Enter a '%s' for team '%s'", innerText, options.TeamSlug), "")

			if err != nil {
				err = fmt.Errorf("error while prompting for '%s' for team '%s': %v", innerText, options.TeamSlug, err)
				return
			}

			if existingValue == "" {
				err = fmt.Errorf("input is required")
				return
			}

			options.TemplateVariables[innerText] = existingValue
		}

		output = strings.ReplaceAll(output, fmt.Sprintf("{%s}", innerText), existingValue)
		curIndex = braceIndex + endIndex + 2
	}

	return
}
