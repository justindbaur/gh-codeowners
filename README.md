# gh-codeowners

A GitHub (`gh`) extension for using the `CODEOWNERS` file to make strategic commits.

## Install

```bash
gh extension install justindbaur/gh-codeowners
```

## Commands

### report

Run `gh codeowners report` to get a report of how many files each team owns in your current working tree.
Run `gh codeowners report [team]` to list the files in the working tree owned by that team.
Use `gh codeowners report --unowned` to list unowned files in the working tree.

### stage

Run `gh codeowners stage [team]` to stage all files for a given team.

### auto-pr

Run `gh codeowners auto-pr` to run through an interactive shell for quickly creating PR's for multiple teams.
Use `--validate '<command>'` to run a command after staging each isolated
changeset and before committing it. Commands run with `sh -c` on Unix-like
systems and `cmd.exe /C` on Windows. For example:
`gh codeowners auto-pr --validate 'go test ./...'`.

When neither `--draft` nor `--dry-run` is supplied, auto-pr asks whether to
create ready-for-review PRs, draft all PRs, draft only the separate PR, or run
in dry-run mode.

Use `--draft=all` to create every pull request as a draft, or
`--draft=seperate` to create only the unowned-files separate pull request as a
draft. Use `--draft=none` to explicitly create no draft pull requests. The
bare `--draft` form is equivalent to `--draft=all`.

Repeat `--label` to add labels to every created pull request, for example:
`gh codeowners auto-pr --label bug --label "needs review"`.
