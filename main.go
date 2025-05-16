package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/cli/safeexec"
)

type ownerEntry struct {
	file    string
	owners  []string
	matcher regexp.Regexp
}

func parseCodeowners(filePath string) ([]ownerEntry, error) {
	file, err := os.Open(filePath)

	if err != nil {
		return nil, fmt.Errorf("file %s could not be opened", filePath)
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)

	ownerEntries := []ownerEntry{}

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

		ownerEntries = append(ownerEntries, ownerEntry{file: splitLine[0], owners: splitLine[1:], matcher: *regex})
	}

	slices.Reverse(ownerEntries)
	return ownerEntries, nil
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
			for _, entry := range codeowners {
				if entry.matcher.Match(edittedFilesScanner.Bytes()) {
					// They are a match
					if len(entry.owners) == 1 {
						owner := entry.owners[0]
						existingValue, found := singleOwnerReport[owner]

						if found {
							singleOwnerReport[owner] = existingValue + 1
						} else {
							singleOwnerReport[owner] = 1
						}
					} else {
						fmt.Printf("File '%s' is owned by multiple teams %s", edittedFilesScanner.Text(), strings.Join(entry.owners, ", "))
					}
					break
				}
			}
			// TODO: Could do something about unowned files here
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
			for _, entry := range codeowners {
				if entry.matcher.Match(edittedFilesScanner.Bytes()) {
					if slices.Contains(entry.owners, team) {
						foundFileToStage = true
						err := exec.Command(gitBin, "add", edittedFilesScanner.Text()).Run()
						if err != nil {
							fmt.Printf("Failed to stage '%s'\n", edittedFilesScanner.Text())
						}
					}
				}
			}
		}

		if !foundFileToStage {
			fmt.Printf("Did not find any files owned by '%s' run gh codeowners report to see who all owns file in your editted files.\n", team)
		}

		return
	}
}

// Could add a lot more flexibility here
var possibleLocations = [1]string{".github/CODEOWNERS"}

func getCodeownersInfo() ([]ownerEntry, error) {
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
