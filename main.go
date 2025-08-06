package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/safeexec"

	"github.com/justindbaur/gh-codeowners/cmd"
)

var remoteRE = regexp.MustCompile(`(.+)\s+(.+)\s+\((push|fetch)\)`)

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
		GetRemoteName: func() (string, error) {
			remoteOutput, err := exec.Command(gitBin, "remote", "-v").Output()

			if err != nil {
				return "", err
			}

			remoteLines := strings.Split(strings.TrimSuffix(string(remoteOutput), "\n"), "\n")
			validRemotes := make([]string, 0)
			for _, r := range remoteLines {
				match := remoteRE.FindStringSubmatch(r)

				urlType := strings.TrimSpace(match[3])

				// Only add remotes that we can push to
				if urlType == "push" {
					validRemotes = append(validRemotes, strings.TrimSpace(match[1]))
				}
			}

			if len(validRemotes) == 0 {
				return "", fmt.Errorf("repository does not have any configured remotes")
			}

			if len(validRemotes) == 1 {
				return validRemotes[1], nil
			}

			index, err := p.Select("What remote would you like to make PR's on?", validRemotes[0], validRemotes)

			if err != nil {
				return "", err
			}

			return validRemotes[index], nil
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
