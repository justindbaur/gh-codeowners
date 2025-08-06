package cmd

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
)

type Author struct {
	Login string `json:"login"`
}

type PullRequestFile struct {
	Path string `json:"path"`
}

type ReviewRequest struct {
	Type string `json:"__typename"`
}

type TeamReviewRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	ReviewRequest
}

type UserReviewRequest struct {
	Login string `json:"login"`
	ReviewRequest
}

type PullRequest struct {
	Author            Author            `json:"author"`
	Files             []PullRequestFile `json:"files"`
	Number            int               `json:"number"`
	ReviewRequests    []any             `json:"-"`
	RawReviewRequests []json.RawMessage `json:"reviewRequests"`
}

type Member struct {
	Login string `json:"login"`
}

func (pr *PullRequest) UnmarshalJSON(b []byte) error {
	type pullRequest PullRequest

	err := json.Unmarshal(b, (*pullRequest)(pr))

	if err != nil {
		return err
	}

	for _, raw := range pr.RawReviewRequests {
		var rr ReviewRequest
		err = json.Unmarshal(raw, &rr)
		if err != nil {
			return err
		}

		var i any
		switch rr.Type {
		case "Team":
			i = &TeamReviewRequest{}
		case "User":
			i = &UserReviewRequest{}
		default:
			return fmt.Errorf("unknown review request type: %s", rr.Type)
		}

		err = json.Unmarshal(raw, i)
		if err != nil {
			return err
		}

		pr.ReviewRequests = append(pr.ReviewRequests, i)
	}

	return nil
}

func newCmdAudit(opts *RootCmdOptions) *cobra.Command {
	return &cobra.Command{
		Use:  "audit team",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			team := args[0]

			codeowners, err := GetCodeowners(cmd, opts)

			if err != nil {
				return err
			}

			// TODO: Make this testable
			repository, err := repository.Current()

			if err != nil {
				return fmt.Errorf("issue getting current repository: %v", err)
			}

			teamMembersArgs := []string{
				"api",
				fmt.Sprintf("/orgs/%s/teams/%s/members", repository.Owner, team),
			}

			teamMembersOut, teamMembersErr, err := opts.GhExec(teamMembersArgs...)

			if err != nil {
				cmd.OutOrStdout().Write(teamMembersOut.Bytes())
				cmd.ErrOrStderr().Write(teamMembersErr.Bytes())
				return fmt.Errorf("issue getting members team: %v", err)
			}

			// Find people in the given team
			teamMembers := []Member{}

			json.Unmarshal(teamMembersOut.Bytes(), &teamMembers)

			teamMemberLogins := make([]string, len(teamMembers))

			for i, m := range teamMembers {
				teamMemberLogins[i] = m.Login
			}

			fmt.Printf("team members: %v\n", teamMemberLogins)

			// TODO: Allow customization of search query
			prSearchArgs := []string{
				"pr",
				"list",
				"--json",
				"author,number,reviewRequests,files",
				"--limit", // TODO: Remove limit
				"50",
			}
			searchOut, searchErr, err := opts.GhExec(prSearchArgs...)

			if err != nil {
				cmd.OutOrStdout().Write(searchOut.Bytes())
				cmd.ErrOrStderr().Write(searchErr.Bytes())
				return fmt.Errorf("issue listing PR's")
			}

			pullRequests := []PullRequest{}

			err = json.Unmarshal(searchOut.Bytes(), &pullRequests)

			if err != nil {
				return fmt.Errorf("problem unmarshalling PR list response: %v", err)
			}

			ourFiles := map[string]int{}

			// TODO: This needs to be a lot smarter
			codeownersStyleTeamName := fmt.Sprintf("@%s", team)

			for _, pr := range pullRequests {
				// fmt.Printf("PR %d author %s\n", pr.Number, pr.Author.Login)
				// Skip the PR if it was authored by someone in the given team
				if slices.Contains(teamMemberLogins, pr.Author.Login) {
					// TODO: We could evaluate what files in this PR were NOT owned by the given team
					// fmt.Println("Skipping PR, it is authored by a member of the given team")
					continue
				}

				for _, file := range pr.Files {
					fmt.Printf("Checking ownership of file '%s' in PR %d\n", file.Path, pr.Number)
					if codeowners.IsOwnedBy([]byte(file.Path), codeownersStyleTeamName) {
						// TODO: Do something smarter with team name
						AddOrUpdate(ourFiles, file.Path, 1, func(existing int) int {
							return existing + 1
						})
					}
				}
				// fmt.Printf("Done checking PR %d\n", pr.Number)
			}

			// TODO: Do a sort on this
			for file, instances := range ourFiles {
				cmd.Printf("%s: %d\n", file, instances)
			}

			return nil
		},
	}
}
