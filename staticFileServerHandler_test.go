package spaserve

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
)

func TestStaticFilesHandler(t *testing.T) {
	ctx := context.TODO()
	filesys := os.DirFS(path.Join("testdata", "files"))

	t.Run("ServeIndexHTML", func(t *testing.T) {
		handler, err := StaticFilesHandler(ctx, filesys)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that index.html is served
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("ServeExistingFile", func(t *testing.T) {
		handler, err := StaticFilesHandler(ctx, filesys)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that the existing file is served
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("ServeNonExistingFile", func(t *testing.T) {
		handler, err := StaticFilesHandler(ctx, filesys)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/nonexistent.txt", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that a 404 error is returned for non-existing file
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status code %d, but got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("ServeIndexOnNonExistingFile", func(t *testing.T) {
		handler, err := StaticFilesHandler(ctx, filesys)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/page", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("Expected status code %d, but got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("Serve with BasePath", func(t *testing.T) {
		tt := []struct {
			basePath string
			want     string
		}{
			{
				basePath: "",
				want:     "/",
			},
			{
				basePath: "static",
				want:     "/static/",
			},
			{
				basePath: "/static",
				want:     "/static/",
			},
			{
				basePath: "static/",
				want:     "/static/",
			},
			{
				basePath: "/static/",
				want:     "/static/",
			},
			{
				basePath: "static/static",
				want:     "/static/static/",
			},
			{
				basePath: "/static/static",
				want:     "/static/static/",
			},
			{
				basePath: "static/static/",
				want:     "/static/static/",
			},
			{
				basePath: "/static/static/",
				want:     "/static/static/",
			},
		}

		opts := staticFilesHandlerOpts{}

		for _, tc := range tt {
			// Call the WithBasePath function
			result := WithBasePath(tc.basePath)(opts)

			// Assert that the base path is set correctly
			if result.basePath != tc.want {
				t.Errorf("Expected base path to be set to %q, but got %q", tc.want, result.basePath)
			}
		}

		handler, err := StaticFilesHandler(ctx, filesys, WithBasePath("/static"))
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/static/file.txt", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that the existing file is served with base path
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Serve with Logger", func(t *testing.T) {
		bufout := new(bytes.Buffer)
		logger := slog.New(slog.NewJSONHandler(bufout, &slog.HandlerOptions{}))

		// Test WithLogger function
		wo := WithLogger(logger)
		result := wo(staticFilesHandlerOpts{})
		if result.logger != logger {
			t.Errorf("Expected logger to be set to %v, but got %v", logger, result.logger)
		}

		// Call the StaticFilesHandler function with the logger
		handler, err := StaticFilesHandler(ctx, filesys, wo)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/404.txt", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that the existing file is served with base path
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}

		// Assert that the logger is used
		if bufout.Len() == 0 {
			t.Error("Expected logger to be used")
		}

		// Assert that the logger is used
		if !strings.Contains(bufout.String(), "404.txt") {
			t.Errorf("Expected logger to log the request path, but got %q", bufout.String())
		}
	})

	t.Run("Serve with CustomErrorHandler", func(t *testing.T) {
		errorMsg := "Custom error message"
		customErrorHandler := func(statusCode int) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
				w.Write([]byte(errorMsg))
			})
		}
		wo := WithMuxErrorHandler(customErrorHandler)

		result := wo(staticFilesHandlerOpts{})
		if result.muxErrHandler == nil {
			t.Error("Expected mux error handler to be set, but got nil")
		}

		handler, err := StaticFilesHandler(ctx, filesys, WithMuxErrorHandler(customErrorHandler))
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/404.txt", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// Assert that the custom error handler is used
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}

		// Assert that the custom error handler is used
		if strings.Compare(w.Body.String(), http.StatusText(http.StatusNotFound)) == 0 {
			t.Errorf("Expected response body %q, but got %q", errorMsg, w.Body.String())
		}
	})

	t.Run("Serve WithInjectEnv", func(t *testing.T) {
		env := struct {
			Name string `json:"name"`
		}{
			Name: "test",
		}
		namespace := "TEST_ENV"

		tt := []struct {
			name   string
			webEnv any
			ns     string
		}{
			{
				name:   "WithInjectWebEnv",
				webEnv: env,
				ns:     namespace,
			},
			{
				name:   "WithInjectWebEnv with default namespace",
				webEnv: env,
				ns:     "",
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				result := WithInjectWebEnv(tc.webEnv, tc.ns)(staticFilesHandlerOpts{})

				nswant := tc.ns
				if tc.ns == "" {
					nswant = defaultStaticFilesHandlerOpts.ns
				}

				// Assert that the web environment is injected correctly
				if result.webEnv != tc.webEnv {
					t.Errorf("Expected web environment to be injected as %v, but got %v", env, result.webEnv)
				}

				// Assert that the namespace is set correctly
				if strings.Compare(result.ns, nswant) != 0 {
					t.Errorf("Expected namespace to be set to %q, but got %q", nswant, result.ns)
				}
			})
		}

		// Call the StaticFilesHandler function with the web environment
		handler, err := StaticFilesHandler(ctx, filesys, WithInjectWebEnv(env, namespace))
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		fmt.Printf("Response: %s\n", w.Body.String())

		// Assert that the index.html is served
		if w.Code != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, w.Code)
		}

		// Assert that the web environment is injected
		if !strings.Contains(w.Body.String(), env.Name) {
			t.Errorf("Expected web environment to be injected, but got %q", w.Body.String())
		}

	})
}
