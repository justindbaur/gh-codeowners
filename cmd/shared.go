package cmd

import (
	"bufio"
	"bytes"
	"fmt"

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

		return codeowners.FromReader(file.Reader())
	}

	return nil, fmt.Errorf("could not locate a CODEOWNERS file")
}

func GetEdittedFilesScanner(cmd *cobra.Command, opts *RootCmdOptions) (*bufio.Scanner, error) {
	// TODO: Use flag maybe
	diffOutput, err := opts.GitExec("status", "--untracked-files=all", "--null")

	if err != nil {
		return nil, fmt.Errorf("error finding files in the working tree")
	}

	scanner := bufio.NewScanner(bytes.NewReader(diffOutput))
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		if i := bytes.IndexByte(data, 0); i >= 0 {
			// We have a null terminated byte
			line := data[0:i]

			if s := bytes.LastIndexByte(line, ' '); s >= 0 {
				return i + 1, line[s+1 : i], nil
			}
		}

		return 0, nil, nil
	})

	return scanner, nil
}
