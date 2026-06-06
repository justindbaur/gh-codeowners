package codeowners

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromReader(t *testing.T) {
	codeowners, err := FromReader(bytes.NewBufferString(`# Comment line
test-dir/test @team-1
`))

	assert.NoError(t, err)
	assert.NotNil(t, codeowners)

	assert.True(t, codeowners.IsOwnedBy([]byte("test-dir/test/file.txt"), "@team-1"))
}

func TestFromReader_patterns(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		fileName   string
		wantOwners []string
	}{
		// Single-segment (no leading slash) matches at any depth
		{
			name:       "single segment extension matches file at root",
			content:    "*.go @owner",
			fileName:   "main.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single segment extension matches file in subdirectory",
			content:    "*.go @owner",
			fileName:   "cmd/main.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single segment extension matches file deeply nested",
			content:    "*.go @owner",
			fileName:   "a/b/c/d/file.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single segment does not match different extension",
			content:    "*.go @owner",
			fileName:   "file.txt",
			wantOwners: []string{},
		},
		{
			name:       "single segment extension does not match similar extension",
			content:    "*.go @owner",
			fileName:   "file.gorilla",
			wantOwners: []string{},
		},
		{
			name:       "single segment directory matches direct child",
			content:    "test-dir @owner",
			fileName:   "test-dir/file.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single segment directory matches nested child",
			content:    "test-dir @owner",
			fileName:   "test-dir/sub/nested/file.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single segment directory matches anywhere in tree",
			content:    "test-dir @owner",
			fileName:   "parent/test-dir/file.txt",
			wantOwners: []string{"@owner"},
		},

		// Leading slash makes pattern root-relative
		{
			name:       "leading slash matches at root",
			content:    "/docs/file.txt @owner",
			fileName:   "docs/file.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "leading slash does not match in subdirectory",
			content:    "/docs/file.txt @owner",
			fileName:   "other/docs/file.txt",
			wantOwners: []string{},
		},
		{
			name:       "leading slash directory matches children",
			content:    "/src @owner",
			fileName:   "src/main.go",
			wantOwners: []string{"@owner"},
		},

		// Trailing slash means directory/**
		{
			name:       "trailing slash matches direct children",
			content:    "docs/ @owner",
			fileName:   "docs/readme.md",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "trailing slash matches deeply nested children",
			content:    "src/ @owner",
			fileName:   "src/a/b/c/d/file.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "root trailing slash matches children",
			content:    "/docs/ @owner",
			fileName:   "docs/api/reference.md",
			wantOwners: []string{"@owner"},
		},

		// Double-star (**) wildcard
		{
			name:       "double star at start matches any leading path",
			content:    "**/logs/*.txt @owner",
			fileName:   "deep/nested/logs/app.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "double star at start matches file at root",
			content:    "**/logs/*.txt @owner",
			fileName:   "logs/app.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "double star at end matches any trailing path",
			content:    "src/** @owner",
			fileName:   "src/a/b/c/file.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "double star at end matches direct child",
			content:    "src/** @owner",
			fileName:   "src/file.go",
			wantOwners: []string{"@owner"},
		},

		// Single-star (*) does not cross directory separator
		{
			name:       "single star matches within segment",
			content:    "/src/test_*.go @owner",
			fileName:   "src/test_foo.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "single star does not cross directory separator",
			content:    "/src/*.go @owner",
			fileName:   "src/sub/file.go",
			wantOwners: []string{},
		},

		// Question mark (?) matches exactly one non-separator character
		{
			name:       "question mark matches single character",
			content:    "/file?.txt @owner",
			fileName:   "file1.txt",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "question mark does not match multiple characters",
			content:    "/file?.txt @owner",
			fileName:   "file12.txt",
			wantOwners: []string{},
		},
		{
			name:       "question mark does not match path separator",
			content:    "/dir?/file.txt @owner",
			fileName:   "di/r/file.txt",
			wantOwners: []string{},
		},

		// Multiple owners
		{
			name:       "two owners are both returned",
			content:    "*.go @team-1 @team-2",
			fileName:   "main.go",
			wantOwners: []string{"@team-1", "@team-2"},
		},
		{
			name:       "three owners are all returned",
			content:    "*.go @team-1 @team-2 @team-3",
			fileName:   "main.go",
			wantOwners: []string{"@team-1", "@team-2", "@team-3"},
		},

		// Inline comments are stripped from owners
		{
			name:       "inline comment is stripped from owners",
			content:    "*.go @owner # this is a comment",
			fileName:   "file.go",
			wantOwners: []string{"@owner"},
		},
		{
			name:       "inline comment with multiple owners is stripped",
			content:    "*.go @owner1 @owner2 # ownership details",
			fileName:   "file.go",
			wantOwners: []string{"@owner1", "@owner2"},
		},

		// Last matching rule wins (entries are reversed so last match is checked first)
		{
			name:       "later rule overrides earlier rule",
			content:    "*.go @team-1\nfile.go @team-2",
			fileName:   "file.go",
			wantOwners: []string{"@team-2"},
		},
		{
			name:       "earlier rule applies when no later rule matches",
			content:    "*.go @team-1\nother.go @team-2",
			fileName:   "file.go",
			wantOwners: []string{"@team-1"},
		},
		{
			name:       "multiple overrides: last matching rule wins",
			content:    "*.go @team-1\ndocs/*.go @team-2\ndocs/api.go @team-3",
			fileName:   "docs/api.go",
			wantOwners: []string{"@team-3"},
		},

		// Unowned files
		{
			name:       "file with no matching pattern is unowned",
			content:    "*.go @owner",
			fileName:   "file.txt",
			wantOwners: []string{},
		},
		{
			name:       "empty CODEOWNERS means all files unowned",
			content:    "",
			fileName:   "any/file.txt",
			wantOwners: []string{},
		},
		{
			name:       "comment-only CODEOWNERS means all files unowned",
			content:    "# Just a comment\n# Another comment",
			fileName:   "file.txt",
			wantOwners: []string{},
		},

		// Lines with a pattern but no owners are silently skipped
		{
			name:       "pattern with no owners is skipped",
			content:    "*.go\n*.txt @owner",
			fileName:   "file.go",
			wantOwners: []string{},
		},
		{
			name:       "pattern with no owners does not block other rules",
			content:    "*.go\n*.txt @owner",
			fileName:   "file.txt",
			wantOwners: []string{"@owner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			co, err := FromReader(bytes.NewBufferString(tt.content))
			require.NoError(t, err)
			owners := co.FindOwners([]byte(tt.fileName))
			assert.Equal(t, tt.wantOwners, owners)
		})
	}
}

func TestFromReader_invalidPatterns(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "triple star pattern is invalid",
			content: "*** @owner",
		},
		{
			name:    "triple star in path is invalid",
			content: "docs/*** @owner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromReader(bytes.NewBufferString(tt.content))
			assert.Error(t, err)
		})
	}
}

func TestIsOwnedBy(t *testing.T) {
	co, err := FromReader(bytes.NewBufferString("*.go @team-1\n*.ts @team-2\n"))
	require.NoError(t, err)

	assert.True(t, co.IsOwnedBy([]byte("main.go"), "@team-1"))
	assert.False(t, co.IsOwnedBy([]byte("main.go"), "@team-2"))
	assert.False(t, co.IsOwnedBy([]byte("main.go"), "@nonexistent"))
	assert.True(t, co.IsOwnedBy([]byte("app.ts"), "@team-2"))
	assert.False(t, co.IsOwnedBy([]byte("README.md"), "@team-1"))
	assert.False(t, co.IsOwnedBy([]byte("README.md"), "@team-2"))
}

func TestIsOwnedBy_multipleOwners(t *testing.T) {
	co, err := FromReader(bytes.NewBufferString("*.go @team-1 @team-2\n"))
	require.NoError(t, err)

	assert.True(t, co.IsOwnedBy([]byte("main.go"), "@team-1"))
	assert.True(t, co.IsOwnedBy([]byte("main.go"), "@team-2"))
	assert.False(t, co.IsOwnedBy([]byte("main.go"), "@team-3"))
}

func TestIsOwnedBy_lastMatchWins(t *testing.T) {
	// Second rule narrows ownership; IsOwnedBy must reflect the last match
	co, err := FromReader(bytes.NewBufferString("*.go @team-1\nspecial.go @team-2\n"))
	require.NoError(t, err)

	// special.go is owned by @team-2, not @team-1
	assert.False(t, co.IsOwnedBy([]byte("special.go"), "@team-1"))
	assert.True(t, co.IsOwnedBy([]byte("special.go"), "@team-2"))

	// other .go files are still owned by @team-1
	assert.True(t, co.IsOwnedBy([]byte("main.go"), "@team-1"))
	assert.False(t, co.IsOwnedBy([]byte("main.go"), "@team-2"))
}

func TestReadTeams(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		query     string
		wantTeams []string
	}{
		{
			name: "filters owners by prefix across multiple lines",
			content: `*.go @org/backend @org/frontend @other/team
docs/* @org/docs
`,
			query:     "@org/",
			wantTeams: []string{"@org/backend", "@org/frontend", "@org/docs"},
		},
		{
			name: "deduplicates repeated teams",
			content: `*.go @org/backend
*.ts @org/backend @org/frontend
`,
			query:     "@org/",
			wantTeams: []string{"@org/backend", "@org/frontend"},
		},
		{
			name: "strips inline comments before matching owners",
			content: `*.go @org/backend @org/frontend # primary owners
`,
			query:     "@org/",
			wantTeams: []string{"@org/backend", "@org/frontend"},
		},
		{
			name: "skips comment and malformed lines",
			content: `# comment
*.go
docs/* @org/docs
`,
			query:     "@org/",
			wantTeams: []string{"@org/docs"},
		},
		{
			name: "returns empty when nothing matches prefix",
			content: `*.go @someone/backend @someone/frontend
`,
			query:     "@org/",
			wantTeams: []string{},
		},
		{
			name: "supports exact prefix matches",
			content: `*.go @org/backend @org/backend-ops @org/frontend
`,
			query:     "@org/backend",
			wantTeams: []string{"@org/backend", "@org/backend-ops"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teams := ReadTeams(bytes.NewBufferString(tt.content), tt.query)
			assert.ElementsMatch(t, tt.wantTeams, teams)
		})
	}
}
