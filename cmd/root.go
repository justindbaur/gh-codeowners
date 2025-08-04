package cmd

import (
	"bytes"
	"io"

	"github.com/spf13/cobra"
)

type Prompter interface {
	Input(prompt, defaultValue string) (string, error)
	Select(prompt, defaultValue string, options []string) (int, error)
	Confirm(prompt string, defaultValue bool) (bool, error)
}

type File struct {
	Reader io.Reader
	Close  func() error
}

type RootCmdOptions struct {
	In            io.Reader
	Out           io.Writer
	Err           io.Writer
	ReadFile      func(filePath string) (*File, error)
	GitExec       func(arg ...string) ([]byte, error)
	GhExec        func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error)
	Prompter      Prompter
	AskOne        func(templateContents string, contents any) error
	GetRemoteName func() (string, error)
}

func NewCmdRoot(opts *RootCmdOptions) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "gh-codeowners",
	}

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
