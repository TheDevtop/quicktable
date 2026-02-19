package dkey

/*
	Quicktable
	Support functions for dynamic keys
*/

import (
	"strings"
)

const Delimiter = ":"

// Expand key into dynamic form
func Expand(key string) []string {
	return strings.Split(key, Delimiter)
}

// Merge keys into one
func Merge(key ...string) string {
	return strings.Join(key, Delimiter)
}

func Fuse(prefix string, key string) string {
	return strings.Join([]string{prefix, key}, Delimiter)
}

func Strip(prefix string, key string) string {
	return strings.TrimPrefix(key, prefix)
}

func Clean(key string) bool {
	if strings.HasPrefix(key, Delimiter) || strings.HasSuffix(key, Delimiter) {
		return false
	}
	return true
}

// Check if list is not practically empty
func Constrained(ks []string) bool {
	if len(ks) == 0 {
		return false
	}
	if ks[0] == "" {
		return false
	}
	return true
}
