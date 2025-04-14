package spaserve

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cleanPath is a helper to clean and normalize URL paths for FS lookup.
// It removes the base path and ensures the result is relative and uses '/'.
func cleanPath(basePath, requestPath string) string {
	if basePath != "/" && strings.HasPrefix(requestPath, basePath) {
		requestPath = strings.TrimPrefix(requestPath, basePath)
	}
	// Ensure leading slash is removed for relative FS lookup
	cleaned := path.Clean(requestPath) // Clean removes trailing slash, handles .. etc.
	return strings.TrimPrefix(cleaned, "/")
}

// --- Base Path Handler ---

type basePathHandler struct {
	next     http.Handler
	basePath string // Must start and end with /
}

func newBasePathHandler(next http.Handler, basePath string) http.Handler {
	// Base path validation happens in config, assume valid here (starts/ends with /)
	return &basePathHandler{next: next, basePath: basePath}
}

func (h *basePathHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, h.basePath) {
		// If request doesn't have the required prefix, return Not Found
		// Consider making this behavior configurable (e.g., redirect?)
		http.NotFound(w, r)
		return
	}
	// Create a shallow copy of the request to modify URL path
	// Note: Modifying r.URL directly can have side effects if r is reused.
	// However, stdlib middleware often modifies it directly. For safety:
	// r2 := *r
	// r2.URL = &(*r.URL) // Copy URL struct as well
	// r2.URL.Path = strings.TrimPrefix(r.URL.Path, h.basePath)
	// h.next.ServeHTTP(w, &r2)

	// Simpler approach (common in Go ecosystem): modify in place
	r.URL.Path = "/" + strings.TrimPrefix(r.URL.Path, h.basePath) // Add leading / after trimming prefix
	h.next.ServeHTTP(w, r)
}

// --- Index Page Handler ---

type indexPageHandler struct {
	next http.Handler
}

func newIndexPageHandler(next http.Handler) http.Handler {
	return &indexPageHandler{next: next}
}

func (h *indexPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/index.html") {
		r.URL.Path = strings.TrimSuffix(r.URL.Path, "index.html")
	}

	h.next.ServeHTTP(w, r)
}

// --- SPA Router Handler ---

type spaRouterHandler struct {
	next               http.Handler
	checker            FileChecker
	spaFallbackRelPath string // Relative path within FS
	logger             internalLogger
}

func newSpaRouterHandler(next http.Handler, checker FileChecker, spaFallbackPath string, logger internalLogger) http.Handler {
	return &spaRouterHandler{
		next:               next,
		checker:            checker,
		spaFallbackRelPath: spaFallbackPath, // Assumed cleaned and relative by config
		logger:             logger,
	}
}

func (h *spaRouterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var fsPath = r.URL.Path
	switch {
	case fsPath == "/":
		fsPath = "index.html"
	case strings.HasSuffix(fsPath, "/"):
		fsPath = fsPath[1:] + "index.html"
	case filepath.Ext(fsPath) == "":
		fsPath = fsPath[1:] + "/index.html"
	default:
		fsPath = fsPath[1:]
	}

	if r.URL.Path != "/" && filepath.Ext(r.URL.Path) == "" && !h.checker.Exists(fsPath) {
		// It doesn't exist as a file, assume it's an SPA route.
		h.logger.LogAttrs(r.Context(), slog.LevelDebug, "SPA route detected, serving fallback",
			slog.String("requested_path", r.URL.Path),
			slog.String("fs_path", fsPath),
			slog.String("fallback_path", h.spaFallbackRelPath),
		)
		// Rewrite request path to the SPA fallback file path
		// Need to add back leading slash for http.ServeMux compatibility
		r.URL.Path = "/" + h.spaFallbackRelPath
		// Let the next handler (modifier or file server) serve the fallback file
	}

	// Serve the original path (if it existed) or the rewritten path
	h.next.ServeHTTP(w, r)
}

// --- Modifying Handler ---

type modifyingHandler struct {
	next         http.Handler // The handler that serves files (e.g., http.FileServer)
	originalFs   fs.FS
	targetsMap   map[string]TargetConfig // map[relativePath]TargetConfig
	cache        Cache
	logger       internalLogger
	errHandler   internalErrorHandler
	pendingLocks sync.Map // map[path]*sync.Mutex for concurrent first access
}

func newModifyingHandler(
	next http.Handler,
	originalFs fs.FS,
	targetsMap map[string]TargetConfig,
	cache Cache,
	logger internalLogger,
	errHandler internalErrorHandler,
) http.Handler {
	return &modifyingHandler{
		next:       next,
		originalFs: originalFs,
		targetsMap: targetsMap,
		cache:      cache,
		logger:     logger,
		errHandler: errHandler,
		// pendingLocks initialized implicitly by sync.Map
	}
}

func (h *modifyingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var fsPath = r.URL.Path
	switch {
	case fsPath == "/":
		fsPath = "index.html"
	case strings.HasSuffix(fsPath, "/"):
		fsPath = fsPath[1:] + "index.html"
		h.logger.LogAttrs(ctx, slog.LevelDebug, "Rewrote path to include index.html", slog.String("path", fsPath))
	case filepath.Ext(fsPath) == "":
		fsPath = fsPath[1:] + "/index.html"
		h.logger.LogAttrs(ctx, slog.LevelDebug, "Rewrote path to include index.html", slog.String("path", fsPath))
	default:
		fsPath = fsPath[1:]
	}

	targetConf, isTargeted := h.targetsMap[fsPath]

	// If not a targeted file, delegate to the next handler (http.FileServer)
	if !isTargeted {
		h.next.ServeHTTP(w, r)
		return
	}

	h.logger.LogAttrs(ctx, slog.LevelDebug, "Targeted file requested", slog.String("path", fsPath))

	// Check cache first
	if targetConf.CacheResult {
		if cachedData, found := h.cache.Get(fsPath); found {
			h.logger.LogAttrs(ctx, slog.LevelDebug, "Serving modified file from cache", slog.String("path", fsPath))
			serveContent(w, r, fsPath, cachedData)
			return
		}
	}

	// --- Handle concurrent access for first modification ---
	// Get or create a lock specific to this file path
	pathLockInterface, _ := h.pendingLocks.LoadOrStore(fsPath, &sync.Mutex{})
	pathLock := pathLockInterface.(*sync.Mutex)

	pathLock.Lock()
	defer pathLock.Unlock() // Ensure lock is released

	// Double-check cache after acquiring lock (another goroutine might have finished)
	if targetConf.CacheResult {
		if cachedData, found := h.cache.Get(fsPath); found {
			h.logger.LogAttrs(ctx, slog.LevelDebug, "Serving modified file from cache (post-lock)", slog.String("path", fsPath))
			serveContent(w, r, fsPath, cachedData)
			return
		}
	}
	// --- End concurrent access handling ---

	h.logger.LogAttrs(ctx, slog.LevelDebug, "Performing modification", slog.String("path", fsPath))

	// Read original file
	// Use ReadFile for simplicity if fs.FS supports it.
	originalData, err := fs.ReadFile(h.originalFs, fsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			h.logger.LogAttrs(ctx, slog.LevelWarn, "Targeted file not found in source FS", slog.String("path", fsPath), slog.Any("error", err))
			h.errHandler(http.StatusNotFound, w, r)
		} else {
			h.logger.LogAttrs(ctx, slog.LevelError, "Failed to read source file", slog.String("path", fsPath), slog.Any("error", err))
			h.errHandler(http.StatusInternalServerError, w, r)
		}
		return
	}

	// Apply modifier
	modifiedData, err := targetConf.Modifier.Modify(fsPath, originalData)
	if err != nil {
		h.logger.LogAttrs(ctx, slog.LevelError, "File modification failed", slog.String("path", fsPath), slog.Any("error", err))
		// Consider wrapping the error before logging? Modifier might already do that.
		h.errHandler(http.StatusInternalServerError, w, r)
		return
	}

	// Cache if configured
	if targetConf.CacheResult {
		h.cache.Set(fsPath, modifiedData)
		h.logger.LogAttrs(ctx, slog.LevelDebug, "Stored modification result in cache", slog.String("path", fsPath))
		// Remove lock from map once done? Or keep it for potential future non-cached modifications?
		// For simplicity, keep the lock in the map. It's small.
	} else {
		// If not caching, we should remove the lock from the map to avoid memory leak if the file is requested many times.
		h.pendingLocks.Delete(fsPath)
	}

	// Serve the modified content
	serveContent(w, r, fsPath, modifiedData)
}

// serveContent writes the data to the response writer, setting Content-Type.
func serveContent(w http.ResponseWriter, r *http.Request, filePath string, data []byte) {
	// Determine Content-Type
	contentType := mime.TypeByExtension(path.Ext(filePath))
	if contentType == "" {
		// Default to octet-stream or sniff content? Sniffing is generally better.
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)

	// Consider adding ETag or Last-Modified headers based on content hash/mod time if possible
	// For simplicity, we omit them here. Caching is handled by the MemoryCache.

	// Serve using http.ServeContent for range requests etc? Requires io.ReadSeeker.
	// Simpler: just write the bytes.
	// For more robust serving (range requests, etc.), wrap bytes in a reader seeker:
	http.ServeContent(w, r, path.Base(filePath), time.Time{}, bytes.NewReader(data))
	// w.Header().Set("Content-Length", strconv.Itoa(len(data))) // Set by ServeContent
	// w.WriteHeader(http.StatusOK) // Set by ServeContent
	// w.Write(data) // Handled by ServeContent
}
