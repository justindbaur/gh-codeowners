# gh-codeowners

A GitHub (`gh`) extension for using the `CODEOWNERS` file to make strategic commits.

## Install

```bash
gh extension install justindbaur/gh-codeowners
```

## Commands

### report

Run `gh codeowners report` to get a report of how many files each team owns in your current working tree.

### stage

Run `gh codeowners stage [team]` to stage all files for a given team.

### auto-pr

Run `gh codeowners auto-pr` to run through an interactive shell for quickly creating PR's for multiple teams.
Use `--validate '<command>'` to run a command after staging each isolated
changeset and before committing it. Commands run with `sh -c` on Unix-like
systems and `cmd.exe /C` on Windows. For example:
`gh codeowners auto-pr --validate 'go test ./...'`.
