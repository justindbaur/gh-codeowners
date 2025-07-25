package cmd

import (
	"bufio"
	"bytes"
	"fmt"

	"github.com/justindbaur/gh-codeowners/codeowners"
	"github.com/spf13/cobra"
)

var possibleCodeownersLocations = [1]string{".github/CODEOWNERS"}

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
