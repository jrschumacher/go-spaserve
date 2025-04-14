package spaserve

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

func TestFSFileChecker_Exists(t *testing.T) {
	now := time.Now()
	mockFS := fstest.MapFS{
		"exists.txt":       {Data: []byte("content"), ModTime: now, Mode: 0o644},
		"subdir/nested.js": {Data: []byte("content"), ModTime: now, Mode: 0o644},
		"emptydir":         {ModTime: now, Mode: fs.ModeDir | 0o755},
		"subdir":           {ModTime: now, Mode: fs.ModeDir | 0o755},
	}

	checker := NewFSFileChecker(mockFS)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"Existing File", "exists.txt", true},
		{"Nested File", "subdir/nested.js", true},
		{"Non-Existent File", "notfound.txt", false},
		{"Non-Existent Nested", "other/notfound.txt", false},
		{"Directory", "emptydir", false},      // Exists should return false for dirs
		{"Nested Directory", "subdir", false}, // Exists should return false for dirs
		{"Root Path", ".", false},             // Root is typically treated as a directory
		{"Empty Path", "", false},             // Should likely map to root -> directory
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := checker.Exists(tt.path)
			if exists != tt.expected {
				t.Errorf("Exists(%q) = %v; want %v", tt.path, exists, tt.expected)
			}
		})
	}
}

func TestNewFSFileChecker_NilFS(t *testing.T) {
	// Test that providing nil FS panics
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewFSFileChecker did not panic with nil fs.FS")
		}
	}()
	_ = NewFSFileChecker(nil)
}
