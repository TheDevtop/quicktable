package core

import (
	"strings"
)

type (
	List = []string
	Key  = string
	Pair = map[Key]List
)

const Delimiter = ":"

// Expand key into list
func Expand(k Key) List {
	return strings.Split(k, Delimiter)
}

// Merge list into a single key
func Merge(l List) Key {
	return strings.Join(l, Delimiter)
}

// Check if list is not practically empty
func Constrained(l List) bool {
	if len(l) == 0 {
		return false
	}
	if l[0] == "" {
		return false
	}
	return true
}
