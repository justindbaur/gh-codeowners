package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
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

func FromFile(filePath string) (*Codeowners, error) {
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

		// Handle inline comments
		startOfComment := slices.IndexFunc(splitLine[1:], func(entry string) bool {
			return strings.HasPrefix(entry, "#")
		})

		var owners []string

		if startOfComment == -1 {
			owners = splitLine[1:]
		} else {
			owners = splitLine[1:startOfComment]
		}

		ownerEntries = append(ownerEntries, OwnerEntry{file: splitLine[0], owners: owners, matcher: *regex})
	}

	slices.Reverse(ownerEntries)
	return &Codeowners{entries: ownerEntries}, nil
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
