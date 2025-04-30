package spaserve

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// --- Test Helpers ---

// mockLogger for capturing logs if needed (or use slogtest)
type mockLogger struct {
	lastMsg   string
	lastLevel slog.Level
	mu        sync.Mutex
}

func (m *mockLogger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	m.mu.Lock()
	m.lastMsg = msg
	m.lastLevel = level
	m.mu.Unlock()
	// In real tests, might buffer attrs or use channels
}

func newTestLogger() *mockLogger {
	return &mockLogger{}
}

// mockErrorHandler for checking status codes
type mockErrorHandler struct {
	lastCode int
	mu       sync.Mutex
}

func (m *mockErrorHandler) Handle(statusCode int, w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.lastCode = statusCode
	m.mu.Unlock()
	// Just record, don't write response for easier testing of upstream handlers
	// http.Error(w, http.StatusText(statusCode), statusCode)
}

func newTestErrorHandler() *mockErrorHandler {
	meh := &mockErrorHandler{}
	return meh
}
func (m *mockErrorHandler) getHandlerFunc() internalErrorHandler {
	return m.Handle
}

// mockFileChecker for SPA handler tests
type mockFileChecker struct {
	existingFiles map[string]bool // map paths to existence (true=exists, false=doesn't)
}

func (m *mockFileChecker) Exists(name string) bool {
	exists, found := m.existingFiles[name]
	return found && exists
}

// mockModifier for modifying handler tests
type mockMod struct {
	err       error
	transform func(p string, d []byte) []byte
	callCount int
	mu        sync.Mutex
}

func (m *mockMod) Modify(path string, originalContent []byte) ([]byte, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if m.transform != nil {
		return m.transform(path, originalContent), nil
	}
	return originalContent, nil
}
func (m *mockMod) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// --- BasePath Handler Tests ---

func TestBasePathHandler(t *testing.T) {
	tests := []struct {
		name           string
		basePath       string // Configured base path
		reqPath        string // Request path
		expectedStatus int
		expectedPath   string
	}{
		{"root with base request", "/", "/", http.StatusOK, "/"},
		{"root with file request", "/", "/file.txt", http.StatusOK, "/file.txt"},
		{"dir with base request", "/app/", "/app/", http.StatusOK, "/"},
		{"dir with file request", "/app/", "/app/file.txt", http.StatusOK, "/file.txt"},
		{"dir without matching base", "/app/", "/", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.expectedPath {
					w.WriteHeader(http.StatusOK)
				} else {
					t.Errorf("Expected path %q, got %q", tt.expectedPath, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
				if _, err := w.Write([]byte(r.URL.Path)); err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			})
			handler := newBasePathHandler(nextHandler, tt.basePath)
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tt.reqPath, nil)

			handler.ServeHTTP(rr, req)
		})
	}
}

// --- SPA Router Handler Tests ---

func TestSpaRouterHandler(t *testing.T) {
	// Next handler just records the path it received
	var receivedPath string
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	checker := &mockFileChecker{
		existingFiles: map[string]bool{
			"exists.js":        true,
			"spa/index.html":   true, // The fallback path itself
			"assets/style.css": true,
			"api/users":        false, // Simulate an API path that doesn't exist as a file
			"about":            false, // Simulate an SPA route that doesn't exist as file
		},
	}
	logger := newTestLogger()
	fallbackPath := "spa/index.html" // Relative path

	handler := newSpaRouterHandler(nextHandler, checker, fallbackPath, logger)

	tests := []struct {
		name           string
		reqPath        string // Assumes base path already stripped
		expectedPath   string // Path expected by the *next* handler
		expectedStatus int
	}{
		{"Existing File with Ext", "/exists.js", "/exists.js", http.StatusOK},
		{"Existing Fallback File", "/spa/index.html", "/spa/index.html", http.StatusOK},
		{"Existing Nested File", "/assets/style.css", "/assets/style.css", http.StatusOK},
		{"Non-Existent File with Ext", "/notfound.css", "/notfound.css", http.StatusOK},           // Fallback NOT triggered
		{"Non-Existent No Ext (SPA Route)", "/about", "/" + fallbackPath, http.StatusOK},          // Fallback triggered
		{"Non-Existent Nested No Ext", "/user/profile", "/" + fallbackPath, http.StatusOK},        // Fallback triggered
		{"Root Path", "/", "/", http.StatusOK},                                                    // Fallback NOT triggered for root
		{"Non-Existent but looks like API Path", "/api/users", "/" + fallbackPath, http.StatusOK}, // Fallback triggered (no extension)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receivedPath = "" // Reset recorder
			rr := httptest.NewRecorder()
			// Request path assumes base path stripping happened before this handler
			req := httptest.NewRequest("GET", tt.reqPath, nil)

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
			if receivedPath != tt.expectedPath {
				t.Errorf("Next handler expected path %q, but received %q", tt.expectedPath, receivedPath)
			}
		})
	}
}

// --- Modifying Handler Tests ---

func TestModifyingHandler(t *testing.T) {
	// Next handler (base file server mock) - returns content based on path
	mockFS := fstest.MapFS{
		"static.txt":        {Data: []byte("Static Content")},
		"target.html":       {Data: []byte("<html>Original</html>")},
		"target-nocache.js": {Data: []byte("original_js()")},
		"only-read.css":     {Data: []byte("body{color:red}")},
	}
	nextHandler := http.FileServer(http.FS(mockFS)) // Use real file server on mock FS

	cache := NewMemoryCache()
	logger := newTestLogger()
	errHandler := newTestErrorHandler()

	// Modifier adds suffix, tracks calls
	modifier := &mockMod{
		transform: func(p string, d []byte) []byte {
			return append(d, []byte("::MODIFIED")...)
		},
	}
	// Modifier that fails
	failingModifier := &mockMod{err: errors.New("MOD_FAIL")}

	targets := map[string]TargetConfig{
		"target.html":       {TargetFile: "target.html", Modifier: modifier, CacheResult: true},
		"target-nocache.js": {TargetFile: "target-nocache.js", Modifier: modifier, CacheResult: false},
		"missing.txt":       {TargetFile: "missing.txt", Modifier: modifier, CacheResult: true},     // File doesn't exist in FS
		"fail.txt":          {TargetFile: "fail.txt", Modifier: failingModifier, CacheResult: true}, // File exists, mod fails
	}

	handler := newModifyingHandler(nextHandler, mockFS, targets, cache, logger, errHandler.getHandlerFunc())

	// --- Test Cases ---
	t.Run("Non-Targeted File", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/static.txt", nil)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}
		if body := rr.Body.String(); body != "Static Content" {
			t.Errorf("Expected body %q, got %q", "Static Content", body)
		}
		if modifier.GetCallCount() != 0 { // Modifier shouldn't be called
			t.Error("Modifier was called for non-targeted file")
		}
	})

	t.Run("Targeted File - First Request (Cache Miss)", func(t *testing.T) {
		modifier.callCount = 0 // Reset call count
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/target.html", nil)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}
		expectedBody := "<html>Original</html>::MODIFIED"
		if body := rr.Body.String(); body != expectedBody {
			t.Errorf("Expected body %q, got %q", expectedBody, body)
		}
		if modifier.GetCallCount() != 1 {
			t.Errorf("Expected modifier call count 1, got %d", modifier.GetCallCount())
		}
		// Check cache
		cached, found := cache.Get("target.html")
		if !found {
			t.Error("Expected modified content to be cached")
		}
		if string(cached) != expectedBody {
			t.Error("Cached content mismatch")
		}
	})

	t.Run("Targeted File - Second Request (Cache Hit)", func(t *testing.T) {
		modifier.callCount = 0 // Reset call count
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/target.html", nil) // Request same file again
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
		}
		expectedBody := "<html>Original</html>::MODIFIED"
		if body := rr.Body.String(); body != expectedBody {
			t.Errorf("Expected body %q, got %q", expectedBody, body)
		}
		if modifier.GetCallCount() != 0 { // Modifier should NOT be called
			t.Errorf("Modifier was called on cache hit, call count %d", modifier.GetCallCount())
		}
	})

	t.Run("Targeted File - CacheResult False", func(t *testing.T) {
		modifier.callCount = 0 // Reset call count
		// First request
		rr1 := httptest.NewRecorder()
		req1 := httptest.NewRequest("GET", "/target-nocache.js", nil)
		handler.ServeHTTP(rr1, req1)
		if rr1.Code != http.StatusOK {
			t.Fatalf("Request 1 failed: status %d", rr1.Code)
		}
		if modifier.GetCallCount() != 1 {
			t.Fatalf("Expected modifier call count 1 after first request, got %d", modifier.GetCallCount())
		}
		// Check not cached
		_, found := cache.Get("target-nocache.js")
		if found {
			t.Error("File with CacheResult=false was cached")
		}

		// Second request
		rr2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/target-nocache.js", nil)
		handler.ServeHTTP(rr2, req2)
		if rr2.Code != http.StatusOK {
			t.Fatalf("Request 2 failed: status %d", rr2.Code)
		}
		if modifier.GetCallCount() != 2 { // Should be called again
			t.Errorf("Expected modifier call count 2 after second request, got %d", modifier.GetCallCount())
		}
		expectedBody := "original_js()::MODIFIED"
		if body := rr2.Body.String(); body != expectedBody {
			t.Errorf("Expected body %q, got %q", expectedBody, body)
		}
	})

	t.Run("Targeted File - Not Found in FS", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/missing.txt", nil)
		handler.ServeHTTP(rr, req)

		// modifyingHandler should intercept and call the error handler
		if errHandler.lastCode != http.StatusNotFound {
			t.Errorf("Expected error handler code %d, got %d (RR status: %d)", http.StatusNotFound, errHandler.lastCode, rr.Code)
		}
		// Note: The actual response code might be 0 if the mock error handler doesn't write a header.
	})

	t.Run("Targeted File - Modifier Fails", func(t *testing.T) {
		// Need to add fail.txt to the FS for the handler to attempt modification
		mockFS["fail.txt"] = &fstest.MapFile{Data: []byte("Can read this")}
		defer delete(mockFS, "fail.txt") // Clean up

		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/fail.txt", nil)
		handler.ServeHTTP(rr, req)

		if errHandler.lastCode != http.StatusInternalServerError {
			t.Errorf("Expected error handler code %d, got %d (RR status: %d)", http.StatusInternalServerError, errHandler.lastCode, rr.Code)
		}
	})

	t.Run("Targeted File - Concurrent First Access", func(t *testing.T) {
		cache := NewMemoryCache() // Fresh cache
		concurrentMod := &mockMod{
			transform: func(p string, d []byte) []byte {
				time.Sleep(10 * time.Millisecond) // Simulate work
				return append(d, []byte("::CONCURRENT")...)
			},
		}
		fs := fstest.MapFS{"concurrent.txt": {Data: []byte("Conc")}}
		concTargets := map[string]TargetConfig{
			"concurrent.txt": {Modifier: concurrentMod, CacheResult: true},
		}
		concHandler := newModifyingHandler(http.FileServer(http.FS(fs)), fs, concTargets, cache, logger, errHandler.getHandlerFunc())

		numRequests := 5
		var wg sync.WaitGroup
		wg.Add(numRequests)

		results := make([]*httptest.ResponseRecorder, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(idx int) {
				defer wg.Done()
				rr := httptest.NewRecorder()
				req := httptest.NewRequest("GET", "/concurrent.txt", nil)
				concHandler.ServeHTTP(rr, req)
				results[idx] = rr
			}(i)
		}
		wg.Wait()

		if concurrentMod.GetCallCount() != 1 {
			t.Errorf("Expected modifier to be called exactly once during concurrent access, got %d", concurrentMod.GetCallCount())
		}
		expectedBody := "Conc::CONCURRENT"
		for i, rr := range results {
			if rr.Code != http.StatusOK {
				t.Errorf("Concurrent request %d: Expected status %d, got %d", i, http.StatusOK, rr.Code)
			}
			if body := rr.Body.String(); body != expectedBody {
				t.Errorf("Concurrent request %d: Expected body %q, got %q", i, expectedBody, body)
			}
		}
		// Check cache was populated
		_, found := cache.Get("concurrent.txt")
		if !found {
			t.Error("Expected concurrent modification to populate cache")
		}
	})
}
