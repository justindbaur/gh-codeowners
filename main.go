package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/safeexec"
	"github.com/spf13/cobra"
)

type GitClient interface {
	Command(args ...string) (*exec.Cmd, error)
}

type Prompter interface {
	Input(prompt, defaultValue string) (string, error)
	Select(prompt, defaultValue string, options []string) (int, error)
}

type File struct {
	Reader io.Reader
	Close  func() error
}

type rootCmdOptions struct {
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
	ReadFile func(filePath string) (*File, error)
	GitExec  func(arg ...string) ([]byte, error)
	GhExec   func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error)
	Prompter Prompter
	AskOne   func(templateContents string, contents any) error
}

func main() {
	gitBin, err := safeexec.LookPath("git")

	if err != nil {
		fmt.Printf("error finding path to 'git': %v\n", err)
		os.Exit(1)
	}

	p := prompter.New(os.Stdin, os.Stdout, os.Stderr)

	rootCmdOptions := &rootCmdOptions{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		GitExec: func(arg ...string) ([]byte, error) {
			return exec.Command(gitBin, arg...).Output()
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			return gh.Exec(arg...)
		},
		Prompter: p,
		AskOne: func(templateContents string, contents any) error {
			prompt := &survey.Editor{
				Message:       "PR template:",
				FileName:      "pull_request_template.md",
				Default:       templateContents,
				HideDefault:   true,
				AppendDefault: true,
			}
			return survey.AskOne(prompt, &contents)
		},
		ReadFile: func(filePath string) (file *File, err error) {
			actualFile, err := os.Open(filePath)

			if err != nil {
				return
			}

			return &File{
				Reader: actualFile,
				Close: func() error {
					return actualFile.Close()
				},
			}, nil
		},
	}

	err = mainCore(rootCmdOptions, os.Args[1:])

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Entrypoint that is geared towards being testable
func mainCore(rootCmdOptions *rootCmdOptions, args []string) error {
	cmd := &cobra.Command{
		Use:     "gh codeowners <command>",
		Short:   "GitHub codeowners extension",
		Long:    "Do work efficiently with the context of a CODEOWNERS file.",
		Example: "  $ gh codeowners report\n  $ gh codeowners stage\n  $ gh codeowners auto-pr",
	}

	cmd.SetIn(rootCmdOptions.In)
	cmd.SetOut(rootCmdOptions.Out)
	cmd.SetErr(rootCmdOptions.Err)

	// TODO: Add persistent flag for giving us the location of the codeowners file
	// TODO: Add persistent flag for giving us the list of files to use
	cmd.PersistentFlags().Bool("help", false, "Show help for command")

	// All commands require the codeowners file existence right now
	codeowners, err := getCodeownersInfo(rootCmdOptions)

	if err != nil {
		return fmt.Errorf("error getting codeowners file: %v", err)
	}

	statusOutput, err := rootCmdOptions.GitExec("--no-pager", "diff", "--name-only")

	if err != nil {
		return fmt.Errorf("error finding files in working tree")
	}

	edittedFilesScanner := bufio.NewScanner(bytes.NewReader(statusOutput))

	// report command
	cmd.AddCommand(&cobra.Command{
		Use:     "report",
		Short:   "Report on current working directory",
		Long:    "Show report of the owners of all files in the current working directory",
		Example: "  $ gh codeowners report",
		RunE: func(cmd *cobra.Command, args []string) error {
			singleOwnerReport := map[string]int{}

			// Loop over all editted files
			for edittedFilesScanner.Scan() {
				owners := codeowners.FindOwners(edittedFilesScanner.Bytes())
				if len(owners) == 1 {
					owner := owners[0]
					existingValue, found := singleOwnerReport[owner]

					if found {
						singleOwnerReport[owner] = existingValue + 1
					} else {
						singleOwnerReport[owner] = 1
					}
				} else if len(owners) > 1 {
					cmd.Printf("File '%s' is owned by multiple teams %s\n", edittedFilesScanner.Text(), strings.Join(owners, ", "))
				} else {
					// TODO: Could do something about unowned files here
					existingValue, found := singleOwnerReport[""]

					if found {
						singleOwnerReport[""] = existingValue + 1
					} else {
						singleOwnerReport[""] = 1
					}
				}
			}

			for owner, ownedFiles := range singleOwnerReport {
				if owner == "" {
					cmd.Printf("Files that are unowned: %d\n", ownedFiles)
				} else {
					cmd.Printf("%s: %d\n", owner, ownedFiles)
				}
			}
			return nil
		},
	})

	// stage command
	cmd.AddCommand(&cobra.Command{
		Use:  "stage team",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			team := args[0]

			foundFileToStage := false

			// Do file staging
			for edittedFilesScanner.Scan() {
				if codeowners.IsOwnedBy(edittedFilesScanner.Bytes(), team) {
					foundFileToStage = true
					_, err := rootCmdOptions.GitExec("add", edittedFilesScanner.Text())
					if err != nil {
						return fmt.Errorf("failed to stage '%s': %v", edittedFilesScanner.Text(), err)
					}
					cmd.Printf("Staged: %s\n", edittedFilesScanner.Text())
				}
			}

			if !foundFileToStage {
				return fmt.Errorf("did not find any files owned by '%s' run gh codeowners report to see who all owns file in your editted files", team)
			}

			return nil
		},
	})

	cmd.AddCommand(newAutoPullRequestCmd(edittedFilesScanner, codeowners, rootCmdOptions))

	// cmd.AddCommand(&cobra.Command{
	// 	Use:  "filter team",
	// 	Args: cobra.MaximumNArgs(1),
	// 	RunE: func(cmd *cobra.Command, args []string) error {
	// 		team := cmd.Flags().Args()[0]

	// 		inputScanner := bufio.NewScanner(os.Stdin)

	// 		for inputScanner.Scan() {
	// 			// fmt.Printf("Checking %s\n", inputScanner.Text())

	// 			owners := codeowners.FindOwners(inputScanner.Bytes())

	// 			// Does the given team own this file?
	// 			if !slices.Contains(owners, team) {
	// 				continue
	// 			}

	// 			cmd.Println(inputScanner.Text())
	// 		}

	// 		return nil
	// 	},
	// })

	cmd.SetArgs(args)
	return cmd.Execute()
}

// Could add a lot more flexibility here
var possibleLocations = [1]string{".github/CODEOWNERS"}

func getCodeownersInfo(opts *rootCmdOptions) (*Codeowners, error) {
	for _, location := range possibleLocations {
		file, err := opts.ReadFile(location)

		if err != nil {
			// Not found in that location, try the other ones
			continue
		}

		defer file.Close()

		return FromReader(file.Reader)
	}

	return nil, fmt.Errorf("could not locate a CODEOWNERS file")
}

func GetCodeowners(cmd *cobra.Command) (*Codeowners, error) {
	return nil, errors.New("not implemented")
}

type AutoPROptions struct {
	IsDraft        bool
	CommitTemplate string
	BranchTemplate string
	// TODO: Allow body in non-interactive way

	UnownedFiles string
	DryRun       bool
}

func newAutoPullRequestCmd(edittedFilesScanner *bufio.Scanner, codeowners *Codeowners, rootCmdOptions *rootCmdOptions) *cobra.Command {
	opts := &AutoPROptions{}

	cmd := &cobra.Command{
		Use:     "auto-pr",
		Example: "$ gh codeowners auto-pr",
		RunE: func(cmd *cobra.Command, args []string) error {
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
				optionIndex, err := rootCmdOptions.Prompter.Select(fmt.Sprintf("Choose where to put %d unowned files", len(unownedFiles)), "", options)

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
				branchTemplate = opts.BranchTemplate
			} else {
				var err error
				branchTemplate, err = rootCmdOptions.Prompter.Input("Enter branch template", "")

				if err != nil {
					return fmt.Errorf("error while requesting branch template: %v", err)
				}
			}

			// TODO: Some validation that the branch template is going to be unique?

			prTemplateFilePath, err := rootCmdOptions.Prompter.Input("Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md")

			if err != nil {
				return fmt.Errorf("error while requesting path to PR template: %v", err)
			}

			var commitMessageTemplate string
			if cmd.Flags().Changed("commit") {
				commitMessageTemplate = opts.CommitTemplate
			} else {
				var err error
				commitMessageTemplate, err = rootCmdOptions.Prompter.Input("Enter the commit message you want to use", "")

				if err != nil {
					return fmt.Errorf("error while getting commit message template: %v", err)
				}
			}

			prTemplateFile, err := rootCmdOptions.ReadFile(prTemplateFilePath)

			if err != nil {
				return fmt.Errorf("error while reading PR template file: %v", err)
			}

			prTemplateFileContents := new(strings.Builder)
			_, err = io.Copy(prTemplateFileContents, prTemplateFile.Reader)

			if err != nil {
				return fmt.Errorf("error while reading PR template file contents: %v", err)
			}

			var contents = ""
			err = rootCmdOptions.AskOne(prTemplateFileContents.String(), &contents)

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

				teamBranch, err := formatString(rootCmdOptions.Prompter, branchTemplate, fmtOptions)

				if err != nil {
					return fmt.Errorf("error while formatting branch template: %v", err)
				}

				checkoutArgs := []string{"checkout", "-b", teamBranch}

				// Checkout
				checkoutOutput, err := rootCmdOptions.GitExec(checkoutArgs...)

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
				addOutput, err := rootCmdOptions.GitExec(append([]string{"add"}, files...)...)

				if err != nil {
					cmd.Println("Error doing git add operation")
					cmd.Println(err)
					os.Stdout.Write(addOutput)
					// TODO: Return actual error
					return nil
				}

				cmd.Println("git add")
				os.Stdout.Write(addOutput)

				teamCommitMessage, err := formatString(rootCmdOptions.Prompter, commitMessageTemplate, fmtOptions)

				if err != nil {
					return fmt.Errorf("error while formatting commit message template: %v", err)
				}

				// Create commit
				commitArgs := []string{"commit", "--message", teamCommitMessage}

				commitOutput, err := rootCmdOptions.GitExec(commitArgs...)

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
				pushOutput, err := rootCmdOptions.GitExec(pushArgs...)

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

				newString, err := formatString(rootCmdOptions.Prompter, contents, fmtOptions)

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
				args := []string{"pr", "new", "--body-file", file.Name(), "--title", teamCommitMessage, "--draft"}

				cmd.Println(args)

				stdOut, stdErr, err := rootCmdOptions.GhExec(args...)

				if err != nil {
					return fmt.Errorf("error creating PR with GitHub CLI: %v", err)
				}

				// Should we do anything else here?
				os.Stdout.Write(stdOut.Bytes())
				os.Stderr.Write(stdErr.Bytes())

				// Checkout back to the last branch so we can continue with new teams or leave the user back where they were
				checkoutOutput, err = rootCmdOptions.GitExec("checkout", "-")

				if err != nil {
					return fmt.Errorf("error trying to checkout base branch: %v", err)
				}

				os.Stdout.Write(checkoutOutput)
				cmd.Printf("Finished making PR for %s\n", team)
			}

			return nil
		},
	}

	cmd.SetUsageFunc(func(c *cobra.Command) error {
		c.Println("When defining the commit, branch, or PR template you may use our templated string format for defining rich information per team.")
		c.Println("")
		return nil
	})

	fl := cmd.Flags()
	fl.StringVarP(&opts.CommitTemplate, "commit", "c", "", "The template string to use for each commit")
	fl.StringVarP(&opts.BranchTemplate, "branch", "b", "", "The template string to use for each branch that is created")
	fl.StringVarP(&opts.UnownedFiles, "unowned-files", "u", "", "What PR to put unowned files onto. `separate` to make their own PR.")
	fl.BoolVarP(&opts.IsDraft, "draft", "d", false, "Mark the pull requests as drafts")
	fl.BoolVar(&opts.DryRun, "dry-run", false, "Print details instead of creating the PR. May still push git changes.")

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
