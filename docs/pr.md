# Auto PR

## Templates

In order to make good looking PRs for multiple teams this command makes use
of go templates. The commit/PR title, branch, and PR body all use the same
template data. You could make a PR body like:

```markdown
# My PR
Number: {{ .Number }}
TeamId: {{ .TeamId }}
Name: {{ .Name }}

## File Info
Number of files: {{ len .Files }}
File list:
{{ range .Files }}
{{ . }}
{{ end }}

## Custom info
{{ .Input "custom" }}
{{ .Input "more" }}
```

When filled in this PR could turn into:

```markdown
# My PR
Number: 1
TeamId: @my-org/team-name
Name: team-name

## File info
Number of files: 2
File list:
dir/one.txt
dir/two.txt

## Custom info
User inputted data
Other data
```


You can use as many or as few of these of as you'd like although the branch name is
required to be unique amongst other teams. 
