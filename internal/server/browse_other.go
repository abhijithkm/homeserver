//go:build !windows

package server

import (
	"os"
	"strings"
)

// listRoots has a single root off Windows (development builds only).
func listRoots() []browseEntry {
	return []browseEntry{{Name: "/", Path: "/", Readable: readableDir("/")}}
}

// hiddenDir treats dot-folders as hidden.
func hiddenDir(fi os.FileInfo) bool { return strings.HasPrefix(fi.Name(), ".") }
