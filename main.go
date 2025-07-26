package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/safeexec"

	"github.com/justindbaur/gh-codeowners/cmd"
)

func main() {
	gitBin, err := safeexec.LookPath("git")

	if err != nil {
		fmt.Printf("error finding path to 'git': %v\n", err)
		os.Exit(1)
	}

	p := prompter.New(os.Stdin, os.Stdout, os.Stderr)

	rootCmdOptions := &cmd.RootCmdOptions{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		GitExec: func(arg ...string) ([]byte, error) {
			return exec.Command(gitBin, arg...).Output()
		},
		GhExec: func(arg ...string) (stdout bytes.Buffer, stderr bytes.Buffer, err error) {
			return gh.Exec(arg...)
		},
		Prompter: p,
		AskOne: func(templateContents string, contents any) error {
			prompt := &survey.Editor{
				Message:       "PR template:",
				FileName:      "pull_request_template.md",
				Default:       templateContents,
				HideDefault:   true,
				AppendDefault: true,
			}
			return survey.AskOne(prompt, contents)
		},
		ReadFile: func(filePath string) (file *cmd.File, err error) {
			actualFile, err := os.Open(filePath)

			if err != nil {
				return
			}

			return &cmd.File{
				Reader: actualFile,
				Close: func() error {
					return actualFile.Close()
				},
			}, nil
		},
	}

	err = mainCore(rootCmdOptions, os.Args[1:])

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Entrypoint that is geared towards being testable
func mainCore(rootCmdOptions *cmd.RootCmdOptions, args []string) error {
	rootCmd := cmd.NewCmdRoot(rootCmdOptions)
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}
