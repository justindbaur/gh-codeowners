package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCmdStage(opts *RootCmdOptions) *cobra.Command {
	return &cobra.Command{
		Use: "stage team",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(cmd, args); err != nil {
				return fmt.Errorf("required team argument missing")
			}

			// TODO: Validate that it's a valid team
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			edittedFilesScanner, err := GetEdittedFilesScanner(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting editted files scanner: %v", err)
			}

			codeowners, err := GetCodeowners(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting codeowners info: %v", err)
			}

			team := args[0]

			foundFileToStage := false

			// Do file staging
			for edittedFilesScanner.Scan() {
				if codeowners.IsOwnedBy(edittedFilesScanner.Bytes(), team) {
					foundFileToStage = true
					_, err := opts.GitExec("add", edittedFilesScanner.Text())
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
	}
}
