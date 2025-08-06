package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/justindbaur/gh-codeowners/codeowners"
	"github.com/spf13/cobra"
)

var possibleCodeownersLocations = [3]string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

func GetCodeowners(cmd *cobra.Command, opts *RootCmdOptions) (*codeowners.Codeowners, error) {
	// TODO: Use flag maybe
	for _, location := range possibleCodeownersLocations {
		file, err := opts.ReadFile(location)

		if err != nil {
			// Not found in that location, try the other ones
			continue
		}

		defer file.Close()

		return codeowners.FromReader(file.Reader)
	}

	return nil, fmt.Errorf("could not locate a CODEOWNERS file")
}

func GetEdittedFilesScanner(cmd *cobra.Command, opts *RootCmdOptions) (*bufio.Scanner, error) {
	// TODO: Use flag maybe
	diffOutput, err := opts.GitExec("--no-pager", "diff", "--name-only")

	if err != nil {
		return nil, fmt.Errorf("error finding files in the working tree")
	}

	return bufio.NewScanner(bytes.NewReader(diffOutput)), nil
}

func AddOrUpdate[K comparable, E any](m map[K]E, key K, initialValue E, updater func(existingValue E) E) {
	existingValue, found := m[key]
	if found {
		m[key] = updater(existingValue)
	} else {
		m[key] = initialValue
	}
}

type Team struct {
	org  string
	name string
}

func ParseTeam(value string) (*Team, error) {
	if !(strings.Index(value, "@") == 0) {
		return nil, errors.New("missing @ at the beginning")
	}

	split := strings.Split(value[1:], "/")

	if len(split) != 2 {
		return nil, errors.New("cannot be split in two by /")
	}

	return &Team{org: split[0], name: split[1]}, nil
}

func (t *Team) ShortName() string {
	return t.name
}

func (t *Team) FullName() string {
	return fmt.Sprintf("@%s/%s", t.org, t.name)
}
