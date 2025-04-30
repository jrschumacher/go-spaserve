package spaserve

import (
	"io/fs"
)

// FSFileChecker implements FileChecker using an underlying fs.FS.
type FSFileChecker struct {
	fsys fs.FS
}

// NewFSFileChecker creates a FileChecker for the given filesystem.
func NewFSFileChecker(fsys fs.FS) *FSFileChecker {
	if fsys == nil {
		// Or return an error? Panic seems reasonable for a programming error.
		panic("spaserve: NewFSFileChecker requires a non-nil fs.FS")
	}
	return &FSFileChecker{fsys: fsys}
}

// Exists checks if a file exists and is not a directory.
// Note: This uses fs.Stat. If the fs.FS doesn't support Stat efficiently,
// trying fs.ReadFile might be an alternative, but Stat is preferred.
func (fc *FSFileChecker) Exists(name string) bool {
	stat, err := fs.Stat(fc.fsys, name)
	if err != nil {
		// Doesn't exist or other error (permission etc.) - treat as not existing for SPA routing.
		// We could log fs.ErrPermission specifically if needed.
		return false
	}
	// Ensure it's not a directory
	return !stat.IsDir()
}

// Ensure FSFileChecker implements the interface (compile-time check)
var _ FileChecker = (*FSFileChecker)(nil)
