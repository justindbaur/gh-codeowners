package main

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/safeexec"
)

func main() {
	// First element is path to this binary, second arg is the top level command
	if len(os.Args) < 2 {
		fmt.Println("no top-level command use 'gh codeowners help' to see valid top-level commands")
		return
	}

	topLevelCommand := os.Args[1]

	if strings.EqualFold(topLevelCommand, "help") {
		// TODO: Implement help
		return
	}

	// All leftover commands require the codeowners file existence right now
	codeowners, err := getCodeownersInfo()

	if err != nil {
		fmt.Println(err)
	}

	// All leftover commands also require currently editted files
	gitBin, err := safeexec.LookPath("git")

	if err != nil {
		fmt.Println(err)
		return
	}

	statusOutput, err := exec.Command(gitBin, "--no-pager", "diff", "--name-only").Output()

	if err != nil {
		fmt.Println(err)
		return
	}

	edittedFilesScanner := bufio.NewScanner(bytes.NewReader(statusOutput))

	if strings.EqualFold(topLevelCommand, "report") {
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
				fmt.Printf("File '%s' is owned by multiple teams %s\n", edittedFilesScanner.Text(), strings.Join(owners, ", "))
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
				fmt.Printf("Files that are unowned: %d\n", ownedFiles)
			} else {
				fmt.Printf("%s: %d\n", owner, ownedFiles)
			}
		}

		return
	}

	if strings.EqualFold(topLevelCommand, "stage") {
		// Expect a third argument for the team you want to stage for
		if len(os.Args) != 3 {
			fmt.Println("Expected a third argument to be the team you want to stage files for.")
			return
		}

		team := os.Args[2]

		foundFileToStage := false

		// Do file staging
		for edittedFilesScanner.Scan() {
			if codeowners.IsOwnedBy(edittedFilesScanner.Bytes(), team) {
				foundFileToStage = true
				err := exec.Command(gitBin, "add", edittedFilesScanner.Text()).Run()
				if err != nil {
					fmt.Printf("Failed to stage '%s'\n", edittedFilesScanner.Text())
				}
			}
		}

		if !foundFileToStage {
			fmt.Printf("Did not find any files owned by '%s' run gh codeowners report to see who all owns file in your editted files.\n", team)
		}

		return
	}

	if strings.EqualFold(topLevelCommand, "auto-pr") {
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

		p := prompter.New(os.Stdin, os.Stdout, os.Stderr)

		if len(unownedFiles) > 0 {
			// Let the user choose where to put unowned files
			options := append(slices.Collect(maps.Keys(filesMap)), "Seperate")
			optionIndex, err := p.Select(fmt.Sprintf("Choose where to put %d unowned files", len(unownedFiles)), "", options)

			if err != nil {
				fmt.Println(err)
				return
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
			fmt.Println("There are no files to make PR's for.")
			return
		}

		branchTemplate, err := p.Input("Enter branch template", "")

		if err != nil {
			fmt.Println(err)
			return
		}

		if !strings.Contains(branchTemplate, "{team}") {
			fmt.Println("Branch template does not contain '{team}'")
			return
		}

		prTemplateFile, err := p.Input("Enter the path to the file containing your PR template", "./.github/PULL_REQUEST_TEMPLATE.md")

		if err != nil {
			fmt.Println(err)
			return
		}

		commitMessageTemplate, err := p.Input("Enter the commit message you want to use", "")

		if err != nil {
			fmt.Println(err)
			return
		}

		prTemplateFileContents, err := os.ReadFile(prTemplateFile)

		if err != nil {
			fmt.Println(err)
			return
		}

		prompt := &survey.Editor{
			Message:       "PR template:",
			FileName:      "pull_request_template.md",
			Default:       string(prTemplateFileContents),
			HideDefault:   true,
			AppendDefault: true,
		}

		var contents = ""

		err = survey.AskOne(prompt, &contents)

		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println("Finished prompting the user.")

		// surveyext.Edit(viBin, "pull_request_template", string(prTemplateFileContents), os.Stdin, os.Stdout, os.Stderr)

		// err = editCmd.Wait()

		// if err != nil {
		// 	fmt.Println(err)
		// 	return
		// }

		fmt.Printf("Creating PR's for %d teams\n", len(filesMap))

		// TODO: Do loop over teams
		for team, files := range filesMap {
			fmt.Printf("Creating PR for team: %s\n", team)

			displayName, err := p.Input("Choose display name for team '"+team+"'", team)

			if err != nil {
				fmt.Println(err)
				return
			}

			teamBranch := strings.ReplaceAll(branchTemplate, "{team}", displayName)

			checkoutArgs := []string{"checkout", "-b", teamBranch}

			// Checkout
			checkoutOutput, err := exec.Command(gitBin, checkoutArgs...).Output()

			if err != nil {
				fmt.Println(err)
				return
			}

			fmt.Println("git checkout -b output")
			os.Stdout.Write(checkoutOutput)

			// Stage files for this team
			addOutput, err := exec.Command(gitBin, append([]string{"add"}, files...)...).Output()

			if err != nil {
				fmt.Println(err)
				return
			}

			fmt.Println("git add")
			os.Stdout.Write(addOutput)

			teamCommitMessage := strings.ReplaceAll(commitMessageTemplate, "{team}", displayName)

			// Create commit
			commitArgs := []string{"commit", "--message", teamCommitMessage}

			commitOutput, err := exec.Command(gitBin, commitArgs...).Output()

			if err != nil {
				fmt.Println(err)
				os.Stderr.Write(commitOutput)
				return
			}

			fmt.Println("git commit")
			os.Stdout.Write(commitOutput)

			// Push branch
			// TODO: origin might not be their remote name, we might need to give them an option
			pushArgs := []string{"push", "--set-upstream", "origin", teamBranch}

			// TODO: Currently if the branch exists on remote this fails, show better error or avoid the error in the first place?
			pushOutput, err := exec.Command(gitBin, pushArgs...).Output()

			if err != nil {
				fmt.Println(err)
				os.Stderr.Write(pushOutput)
				return
			}

			fmt.Println("git push")
			os.Stdout.Write(pushOutput)

			// TODO: We could make this safer and remove unsafe path characters
			file, err := os.CreateTemp(os.TempDir(), "team_pr_body_"+displayName)

			if err != nil {
				fmt.Println(err)
				return
			}

			defer file.Close()
			defer os.Remove(file.Name())

			newString := strings.ReplaceAll(contents, "{team}", displayName)

			_, err = file.WriteString(newString)

			if err != nil {
				fmt.Println(err)
				return
			}

			// TODO: Take option for allowing it to create drafts or not
			args := []string{"pr", "new", "--body-file", file.Name(), "--title", teamCommitMessage, "--draft"}

			fmt.Println(args)

			stdOut, stdErr, err := gh.Exec(args...)

			if err != nil {
				fmt.Println(err)
				os.Stderr.Write(stdErr.Bytes())
				return
			}

			// Should we do anything else here?
			os.Stdout.Write(stdOut.Bytes())
			os.Stderr.Write(stdErr.Bytes())

			// Checkout back to the last branch so we can continue with new teams or leave the user back where they were
			checkoutOutput, err = exec.Command(gitBin, "checkout", "-").Output()

			if err != nil {
				fmt.Println(err)
				os.Stdout.Write(checkoutOutput)
				return
			}

			os.Stdout.Write(checkoutOutput)
			fmt.Printf("Finished making PR for %s\n", team)
		}
	}
}

// Could add a lot more flexibility here
var possibleLocations = [1]string{".github/CODEOWNERS"}

func getCodeownersInfo() (*Codeowners, error) {
	for _, location := range possibleLocations {
		_, err := os.Stat(location)

		if err != nil {
			// Not found in that location, try the other ones
			continue
		}

		return FromFile(location)
	}

	return nil, fmt.Errorf("could not locate a CODEOWNERS file")
}
