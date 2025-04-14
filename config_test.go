package spaserve

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"testing/fstest"
)

// Mock Modifier for config testing
type mockModifier struct{}

func (m *mockModifier) Modify(path string, originalContent []byte) ([]byte, error) {
	return originalContent, nil
}

func TestSpaServerConfig_configure(t *testing.T) {
	mockFS := fstest.MapFS{} // Need a non-nil FS

	tests := []struct {
		name        string
		configIn    SpaServerConfig
		expectErr   error           // Use errors.Is for checking
		expectConf  SpaServerConfig // Check specific fields after defaults
		checkLogger bool            // Check if logger gets defaulted
		checkErrH   bool            // Check if error handler gets defaulted
	}{
		{
			name:      "Missing FS",
			configIn:  SpaServerConfig{},
			expectErr: ErrMissingFS,
		},
		{
			name: "Valid Minimal Config",
			configIn: SpaServerConfig{
				FS: mockFS,
			},
			expectErr: nil,
			expectConf: SpaServerConfig{
				FS:              mockFS,
				SpaFallbackPath: "index.html", // Default
				BasePath:        "/",          // Default
			},
			checkLogger: true,
			checkErrH:   true,
		},
		{
			name: "Custom Valid Config",
			configIn: SpaServerConfig{
				FS:              mockFS,
				SpaFallbackPath: "custom/index.page",
				BasePath:        "/app/", // Ensure trailing slash added implicitly if needed
				Targets: []TargetConfig{
					{TargetFile: "/sub/file.js", Modifier: &mockModifier{}, CacheResult: true},
				},
			},
			expectErr: nil,
			expectConf: SpaServerConfig{
				FS:              mockFS,
				SpaFallbackPath: "custom/index.page", // Cleaned
				BasePath:        "/app/",
				Targets: []TargetConfig{
					{TargetFile: "sub/file.js", Modifier: &mockModifier{}, CacheResult: true}, // Cleaned
				},
			},
			checkLogger: true,
			checkErrH:   true,
		},
		{
			name: "Invalid BasePath - No Leading Slash",
			configIn: SpaServerConfig{
				FS:       mockFS,
				BasePath: "app",
			},
			expectErr: ErrInvalidBasePath,
		},
		{
			name: "Valid BasePath - Needs Trailing Slash",
			configIn: SpaServerConfig{
				FS:       mockFS,
				BasePath: "/app", // Should become /app/
			},
			expectErr: nil,
			expectConf: SpaServerConfig{
				FS:              mockFS,
				SpaFallbackPath: "index.html",
				BasePath:        "/app/", // Trailing slash added
			},
			checkLogger: true,
			checkErrH:   true,
		},
		{
			name: "Invalid Target - Missing Modifier",
			configIn: SpaServerConfig{
				FS: mockFS,
				Targets: []TargetConfig{
					{TargetFile: "file.txt"}, // Modifier is nil
				},
			},
			expectErr: ErrConfigTargetMissing,
		},
		{
			name: "Invalid Target - Missing TargetFile",
			configIn: SpaServerConfig{
				FS: mockFS,
				Targets: []TargetConfig{
					{Modifier: &mockModifier{}}, // TargetFile is ""
				},
			},
			expectErr: ErrConfigTargetMissing,
		},
		{
			name: "Custom Logger and Error Handler",
			configIn: SpaServerConfig{
				FS:              mockFS,
				Logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
				MuxErrorHandler: func(i int, w http.ResponseWriter, r *http.Request) {},
			},
			expectErr: nil,
			expectConf: SpaServerConfig{
				FS:              mockFS,
				SpaFallbackPath: "index.html",
				BasePath:        "/",
			},
			checkLogger: false, // Already provided
			checkErrH:   false, // Already provided
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLogger := tt.configIn.Logger
			originalErrHandler := tt.configIn.MuxErrorHandler

			err := tt.configIn.configure()

			if tt.expectErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, but got nil", tt.expectErr)
				} else if !errors.Is(err, tt.expectErr) {
					t.Errorf("Expected error type %v, but got %v", tt.expectErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, but got %v", err)
				}
				// Check defaulted fields
				if tt.configIn.SpaFallbackPath != tt.expectConf.SpaFallbackPath {
					t.Errorf("Expected SpaFallbackPath %q, got %q", tt.expectConf.SpaFallbackPath, tt.configIn.SpaFallbackPath)
				}
				if tt.configIn.BasePath != tt.expectConf.BasePath {
					t.Errorf("Expected BasePath %q, got %q", tt.expectConf.BasePath, tt.configIn.BasePath)
				}
				if tt.checkLogger && tt.configIn.Logger == nil {
					t.Errorf("Expected Logger to be defaulted, but got nil")
				}
				if !tt.checkLogger && tt.configIn.Logger != originalLogger {
					t.Errorf("Logger was modified unexpectedly")
				}
				// Comparing functions directly is tricky, just check nil status
				if tt.checkErrH && tt.configIn.MuxErrorHandler == nil {
					t.Errorf("Expected MuxErrorHandler to be defaulted, but got nil")
				}
				if !tt.checkErrH && tt.configIn.MuxErrorHandler == nil && originalErrHandler != nil {
					// This check is imperfect as we can't easily compare function pointers if defaulted vs provided default
					t.Logf("Warning: Cannot definitively check if provided MuxErrorHandler was preserved if it was nil.")
				}
				if len(tt.configIn.Targets) != len(tt.expectConf.Targets) {
					t.Errorf("Expected %d target(s), got %d", len(tt.expectConf.Targets), len(tt.configIn.Targets))
				} else if len(tt.configIn.Targets) > 0 {
					if tt.configIn.Targets[0].TargetFile != tt.expectConf.Targets[0].TargetFile {
						t.Errorf("Expected target file %q, got %q", tt.expectConf.Targets[0].TargetFile, tt.configIn.Targets[0].TargetFile)
					}
				}
			}
		})
	}
}
