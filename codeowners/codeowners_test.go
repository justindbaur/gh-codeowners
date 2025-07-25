package codeowners

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromReader(t *testing.T) {
	codeowners, err := FromReader(bytes.NewBufferString(`# Comment line
test-dir/test @team-1
`))

	assert.NoError(t, err)
	assert.NotNil(t, codeowners)

	assert.True(t, codeowners.IsOwnedBy([]byte("test-dir/test/file.txt"), "@team-1"))
}
