package main

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/safeexec"
)

type OwnerEntry struct {
	file    string
	owners  []string
	matcher regexp.Regexp
}

type Codeowners struct {
	entries []OwnerEntry
}

func (co Codeowners) FindOwners(fileName []byte) []string {
	for _, entry := range co.entries {
		if entry.matcher.Match(fileName) {
			return entry.owners
		}
	}

	return []string{}
}

func (co Codeowners) IsOwnedBy(fileName []byte, owner string) bool {
	return slices.Contains(co.FindOwners(fileName), owner)
}

type pullRequestTemplate struct {
	Gname string `graphql:"filename"`
	Gbody string `graphql:"body"`
}

func parseCodeowners(filePath string) (*Codeowners, error) {
	file, err := os.Open(filePath)

	if err != nil {
		return nil, fmt.Errorf("file %s could not be opened", filePath)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	ownerEntries := []OwnerEntry{}

	for scanner.Scan() {
		line := scanner.Text()
		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}

		// TODO: More whitespace allowed?
		splitLine := strings.Split(line, " ")

		if len(splitLine) == 0 {
			continue
		}

		if len(splitLine) == 1 {
			// fmt.Printf("Skipping line '%s' as it is improperly formatted.\n", line)
			continue
		}

		filePattern := splitLine[0]

		regex, err := buildPatternRegex(filePattern)

		if err != nil {
			return nil, fmt.Errorf("could not build regex pattern for '%s' %w", filePattern, err)
		}

		ownerEntries = append(ownerEntries, OwnerEntry{file: splitLine[0], owners: splitLine[1:], matcher: *regex})
	}

	slices.Reverse(ownerEntries)
	return &Codeowners{entries: ownerEntries}, nil
}

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
				// TODO: Could do something about unowned files here
				fmt.Printf("File '%s' is owned by multiple teams %s\n", edittedFilesScanner.Text(), strings.Join(owners, ", "))
			}
		}

		for owner, ownedFiles := range singleOwnerReport {
			fmt.Printf("%s: %d\n", owner, ownedFiles)
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

		// TODO: Put unowned files into a group
		if len(filesMap) > 0 {
			// TODO: Could accept unowned files strategy for now just shove them in the first group
			firstKey := slices.Collect(maps.Keys(filesMap))[0]
			filesMap[firstKey] = append(filesMap[firstKey], unownedFiles...)
		} else if len(unownedFiles) > 0 {
			fmt.Println("No team owns any of the files currently in the working directory")
			return
		} else {
			fmt.Println("No files in the working directory")
			return
		}

		p := prompter.New(os.Stdin, os.Stdout, os.Stderr)

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

			args := []string{"pr", "new", "--body-file", file.Name(), "--title", teamCommitMessage}

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

		return parseCodeowners(location)
	}

	return nil, fmt.Errorf("could not locate a CODEOWNERS file")
}

// For more examples of using go-gh, see:
// https://github.com/cli/go-gh/blob/trunk/example_gh_test.go

// Ref: https://github.com/hmarr/codeowners/blob/b0f609d21eb672b5cb2973f47a80210185102504/match.go#L69-L177
func buildPatternRegex(pattern string) (*regexp.Regexp, error) {
	// Handle specific edge cases first
	switch {
	case strings.Contains(pattern, "***"):
		return nil, fmt.Errorf("pattern cannot contain three consecutive asterisks")
	case pattern == "":
		return nil, fmt.Errorf("empty pattern")
	case pattern == "/":
		// "/" doesn't match anything
		return regexp.Compile(`\A\z`)
	}

	segs := strings.Split(pattern, "/")

	if segs[0] == "" {
		// Leading slash: match is relative to root
		segs = segs[1:]
	} else {
		// No leading slash - check for a single segment pattern, which matches
		// relative to any descendent path (equivalent to a leading **/)
		if len(segs) == 1 || (len(segs) == 2 && segs[1] == "") {
			if segs[0] != "**" {
				segs = append([]string{"**"}, segs...)
			}
		}
	}

	if len(segs) > 1 && segs[len(segs)-1] == "" {
		// Trailing slash is equivalent to "/**"
		segs[len(segs)-1] = "**"
	}

	sep := "/"

	lastSegIndex := len(segs) - 1
	needSlash := false
	var re strings.Builder
	re.WriteString(`\A`)
	for i, seg := range segs {
		switch seg {
		case "**":
			switch {
			case i == 0 && i == lastSegIndex:
				// If the pattern is just "**" we match everything
				re.WriteString(`.+`)
			case i == 0:
				// If the pattern starts with "**" we match any leading path segment
				re.WriteString(`(?:.+` + sep + `)?`)
				needSlash = false
			case i == lastSegIndex:
				// If the pattern ends with "**" we match any trailing path segment
				re.WriteString(sep + `.*`)
			default:
				// If the pattern contains "**" we match zero or more path segments
				re.WriteString(`(?:` + sep + `.+)?`)
				needSlash = true
			}

		case "*":
			if needSlash {
				re.WriteString(sep)
			}

			// Regular wildcard - match any characters except the separator
			re.WriteString(`[^` + sep + `]+`)
			needSlash = true

		default:
			if needSlash {
				re.WriteString(sep)
			}

			escape := false
			for _, ch := range seg {
				if escape {
					escape = false
					re.WriteString(regexp.QuoteMeta(string(ch)))
					continue
				}

				// Other pathspec implementations handle character classes here (e.g.
				// [AaBb]), but CODEOWNERS doesn't support that so we don't need to
				switch ch {
				case '\\':
					escape = true
				case '*':
					// Multi-character wildcard
					re.WriteString(`[^` + sep + `]*`)
				case '?':
					// Single-character wildcard
					re.WriteString(`[^` + sep + `]`)
				default:
					// Regular character
					re.WriteString(regexp.QuoteMeta(string(ch)))
				}
			}

			if i == lastSegIndex {
				// As there's no trailing slash (that'd hit the '**' case), we
				// need to match descendent paths
				re.WriteString(`(?:` + sep + `.*)?`)
			}

			needSlash = true
		}
	}
	re.WriteString(`\z`)
	return regexp.Compile(re.String())
}
