package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddOrUpdate_updatesExistingEntry(t *testing.T) {
	myMap := map[string]int{
		"key": 10,
	}

	AddOrUpdate(myMap, "key", 0, func(existing int) int {
		return existing + 1
	})

	assert.Equal(t, 11, myMap["key"])
}

func TestAddOrUpdate_addsNewEntry(t *testing.T) {
	myMap := map[string]int{}

	AddOrUpdate(myMap, "new_key", 0, func(existing int) int {
		return existing + 1
	})

	assert.Equal(t, 0, myMap["new_key"])
}
