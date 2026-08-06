package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newCmdReport(opts *RootCmdOptions) *cobra.Command {
	var unowned bool

	reportCmd := &cobra.Command{
		Use:   "report [team]",
		Short: "Report on current working directory",
		Long:  "Show a report of the owners of all files in the current working directory, or list files owned by a team or unowned files",
		Example: `  $ gh codeowners report
  $ gh codeowners report @org/team
  $ gh codeowners report --unowned`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(cmd, args); err != nil {
				return err
			}
			if unowned && len(args) == 1 {
				return fmt.Errorf("team argument cannot be used with --unowned")
			}
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

			if len(args) == 1 || unowned {
				var team string
				if len(args) == 1 {
					team = args[0]
				}

				for edittedFilesScanner.Scan() {
					owners := codeowners.FindOwners(edittedFilesScanner.Bytes())
					if (unowned && len(owners) == 0) || (!unowned && codeowners.IsOwnedBy(edittedFilesScanner.Bytes(), team)) {
						cmd.Println(edittedFilesScanner.Text())
					}
				}

				return edittedFilesScanner.Err()
			}

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

			owners := make([]string, 0, len(singleOwnerReport))
			for owner := range singleOwnerReport {
				if owner != "" {
					owners = append(owners, owner)
				}
			}
			sort.Strings(owners)

			for _, owner := range owners {
				cmd.Printf("%s: %d\n", owner, singleOwnerReport[owner])
			}
			if unownedFiles := singleOwnerReport[""]; unownedFiles > 0 {
				cmd.Printf("Files that are unowned: %d\n", unownedFiles)
			}
			return nil
		},
	}

	reportCmd.Flags().BoolVar(&unowned, "unowned", false, "List unowned files")
	return reportCmd
}
