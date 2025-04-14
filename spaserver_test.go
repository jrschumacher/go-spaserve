package spaserve

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNewSpaServer_ConfigErrors(t *testing.T) {
	// Test that configuration errors are propagated
	_, err := NewSpaServer(SpaServerConfig{}) // Missing FS
	if !errors.Is(err, ErrMissingFS) {
		t.Errorf("Expected ErrMissingFS, got %v", err)
	}

	_, err = NewSpaServer(SpaServerConfig{FS: fstest.MapFS{}, BasePath: "invalid"})
	if !errors.Is(err, ErrInvalidBasePath) {
		t.Errorf("Expected ErrInvalidBasePath, got %v", err)
	}
}

func TestNewSpaServer_Helpers(t *testing.T) {
	// Test CreateHtmlScriptTagEnvModifier success
	_, err := CreateHtmlScriptTagEnvModifier(map[string]string{}, "VALID_NS")
	if err != nil {
		t.Errorf("CreateHtmlScriptTagEnvModifier failed unexpectedly: %v", err)
	}
	// Test CreateHtmlScriptTagEnvModifier error
	_, err = CreateHtmlScriptTagEnvModifier(map[string]string{}, "invalid-ns")
	if err == nil {
		t.Errorf("CreateHtmlScriptTagEnvModifier expected error for invalid ns, got nil")
	}

	// Test CreateCompositeModifier
	mod := CreateCompositeModifier(&mockModifier{}) // Just check it returns something non-nil
	if mod == nil {
		t.Error("CreateCompositeModifier returned nil")
	}
}

// --- Integration Test for Handler Chain ---
func TestNewSpaServer_Integration(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":       {Data: []byte("<html>Index</html>")},
		"app.js":           {Data: []byte("console.log('app')")},
		"assets/style.css": {Data: []byte("body{margin:0}")},
		// File to be modified
		"config.json": {Data: []byte(`{"old": true}`)},
	}

	// Modifier to simulate changing config.json
	configModifier := &mockMod{
		transform: func(p string, d []byte) []byte {
			return []byte(`{"old": false, "new": true}`)
		},
	}

	config := SpaServerConfig{
		FS:              mockFS,
		BasePath:        "/ui/", // Test with a base path
		SpaFallbackPath: "index.html",
		Targets: []TargetConfig{
			{TargetFile: "config.json", Modifier: configModifier, CacheResult: true},
		},
		// Use default logger/error handler
	}

	handler, err := NewSpaServer(config)
	if err != nil {
		t.Fatalf("NewSpaServer failed: %v", err)
	}

	tests := []struct {
		name           string
		reqPath        string
		expectedStatus int
		expectedBody   string // Check if body contains this string
	}{
		{"SPA Fallback (with trailing slash)", "/ui/some/spa/route/", http.StatusOK, "<html>Index</html>"},
		{"SPA Fallback", "/ui/some/spa/route", http.StatusOK, "<html>Index</html>"},
		{"Index Direct", "/ui/index.html", http.StatusOK, "<html>Index</html>"},
		{"Root Fallback", "/ui/", http.StatusOK, "<html>Index</html>"}, // Should serve index for base path root
		{"Static Asset", "/ui/app.js", http.StatusOK, "console.log('app')"},
		{"Nested Asset", "/ui/assets/style.css", http.StatusOK, "body{margin:0}"},
		{"Modified File", "/ui/config.json", http.StatusOK, `{"old": false, "new": true}`},
		{"Non-Existent Asset", "/ui/notfound.txt", http.StatusNotFound, "404 page not found"}, // Should 404 via fileserver
		{"Outside Base Path", "/other/path", http.StatusNotFound, ""},                         // Should 404 via base path handler
	}

	// --- Run Second Request for Modified File to Test Cache ---
	t.Run("Modified File - Second Request (Cache)", func(t *testing.T) {
		// First request to populate cache (already tested implicitly above, but be explicit)
		rr1 := httptest.NewRecorder()
		req1 := httptest.NewRequest("GET", "/ui/config.json", nil)
		handler.ServeHTTP(rr1, req1)
		if rr1.Code != http.StatusOK {
			t.Fatalf("Cache priming request failed: status %d", rr1.Code)
		}
		if configModifier.GetCallCount() != 1 {
			t.Fatalf("Expected modifier call count 1 after priming, got %d", configModifier.GetCallCount())
		}

		// Second request
		rr2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/ui/config.json", nil)
		handler.ServeHTTP(rr2, req2)

		if rr2.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr2.Code)
		}
		expectedBody := `{"old": false, "new": true}`
		bodyBytes, _ := io.ReadAll(rr2.Body) // Read body fully
		if body := string(bodyBytes); body != expectedBody {
			t.Errorf("Expected body %q, got %q", expectedBody, body)
		}
		// Crucially, check modifier wasn't called again
		if configModifier.GetCallCount() != 1 { // Count should still be 1
			t.Errorf("Modifier was called on cache hit, total calls %d", configModifier.GetCallCount())
		}
	})
	// --- End Cache Test ---

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configModifier.callCount = 0 // Reset for this specific run if needed, though cache test is separate
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.reqPath, nil)

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Request %q: Expected status %d, got %d", tt.reqPath, tt.expectedStatus, rr.Code)
				t.Errorf("Location: %q", rr.Header().Get("Location"))
			}

			bodyBytes, _ := io.ReadAll(rr.Body) // Read body fully
			body := string(bodyBytes)
			if tt.expectedBody != "" && !strings.Contains(body, tt.expectedBody) {
				t.Errorf("Request %q: Expected body to contain %q, got %q", tt.reqPath, tt.expectedBody, body)
				t.Errorf("Location: %q", rr.Header().Get("Location"))
			}
		})
	}

}
