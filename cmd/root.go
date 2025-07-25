package cmd

import (
	"bytes"
	"io"

	"github.com/spf13/cobra"
)

type Prompter interface {
	Input(prompt, defaultValue string) (string, error)
	Select(prompt, defaultValue string, options []string) (int, error)
}

type File struct {
	Reader io.Reader
	Close  func() error
}

type RootCmdOptions struct {
	In       io.Reader
	Out      io.Writer
	Err      io.Writer
	ReadFile func(filePath string) (*File, error)
	GitExec  func(arg ...string) ([]byte, error)
	GhExec   func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error)
	Prompter Prompter
	AskOne   func(templateContents string, contents any) error
}

func NewCmdRoot(opts *RootCmdOptions) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "gh-codeowners",
	}

	rootCmd.SetHelpTemplate(
		`gh-codeowners: Do work efficiently with the context of a CODEOWNERS file.

USAGE:
    gh codeowners <command> [flags]

CORE COMMANDS:
    report:    show a report of who owns the files in the current working tree
    stage:     stage all files in the current working tree that are owned by one team
    auto-pr:   interactively create a PR for each team for all the files in the current working tree

INHERITED FLAGS:
    --help        Show help for command

EXAMPLES:
    $ gh codeowners report
    $ gh codeowners stage @my-team
    $ gh codeowners auto-pr --commit "Commit for {Team Name}" --branch "branch/{Team Name}-do-things"
`)

	rootCmd.SetIn(opts.In)
	rootCmd.SetOut(opts.Out)
	rootCmd.SetErr(opts.Err)

	// TODO: Add persistent flag for giving us the location of the codeowners file
	// TODO: Add persistent flag for giving us the list of files to use
	rootCmd.PersistentFlags().Bool("help", false, "Show help for command")

	rootCmd.AddCommand(newCmdReport(opts))
	rootCmd.AddCommand(newCmdStage(opts))
	rootCmd.AddCommand(newCmdAutoPR(opts))

	return rootCmd
}
