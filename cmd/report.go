package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newCmdReport(opts *RootCmdOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "report",
		Short:   "Report on current working directory",
		Long:    "Show report of the owners of all files in the current working directory",
		Example: "  $ gh codeowners report",
		RunE: func(cmd *cobra.Command, args []string) error {
			edittedFilesScanner, err := GetEdittedFilesScanner(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting editted files scanner: %v", err)
			}

			codeowners, err := GetCodeowners(cmd, opts)

			if err != nil {
				return fmt.Errorf("error getting codeowners info: %v", err)
			}

			singleOwnerReport := map[string]int{}

			// Loop over all editted files
			for edittedFilesScanner.Scan() {
				owners := codeowners.FindOwners(edittedFilesScanner.Bytes())
				if len(owners) == 1 {
					AddOrUpdate(singleOwnerReport, owners[0], 1, func(existing int) int {
						return existing + 1
					})
				} else if len(owners) > 1 {
					cmd.Printf("File '%s' is owned by multiple teams %s\n", edittedFilesScanner.Text(), strings.Join(owners, ", "))
				} else {
					// TODO: Could do something about unowned files here
					AddOrUpdate(singleOwnerReport, "", 1, func(existing int) int {
						return existing + 1
					})
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
	}
}
