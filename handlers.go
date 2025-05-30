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
	"regexp"
	"strings"
	"sync"
	"time"
)

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

// --- HTML Page Whitelist Handler ---

type htmlPageWhitelistHandler struct {
	allowed map[string]bool
	logger  internalLogger
	next    http.Handler
}

func newHtmlPageWhitelistHandler(next http.Handler, allowedHtmlPages []string, logger internalLogger) http.Handler {
	var allowed map[string]bool = make(map[string]bool, len(allowedHtmlPages))
	for _, allowedHtmlPage := range allowedHtmlPages {
		allowed[allowedHtmlPage] = true
	}
	return &htmlPageWhitelistHandler{allowed: allowed, logger: logger, next: next}
}

func (h *htmlPageWhitelistHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	if filepath.Ext(fsPath) != ".html" {
		h.next.ServeHTTP(w, r)
		return
	}
	if _, ok := h.allowed[fsPath]; ok {
		h.next.ServeHTTP(w, r)
		return
	}

	h.logger.LogAttrs(r.Context(), slog.LevelError, "Attempting to fetch an HTML page that is not whitelisted",
		slog.String("path", fsPath),
		slog.String("ext", filepath.Ext(fsPath)),
	)
	http.Error(w, "403 Forbidden", http.StatusForbidden)
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
	next          http.Handler // The handler that serves files (e.g., http.FileServer)
	originalFs    fs.FS
	targetConfigs []TargetConfig // map[relativePath]TargetConfig
	dataCache     Cache[[]byte]
	headerCache   Cache[http.Header]
	logger        internalLogger
	errHandler    internalErrorHandler
	pendingLocks  sync.Map // map[path]*sync.Mutex for concurrent first access
}

func newModifyingHandler(
	next http.Handler,
	originalFs fs.FS,
	targetConfigs []TargetConfig,
	dataCache Cache[[]byte],
	headerCache Cache[http.Header],
	logger internalLogger,
	errHandler internalErrorHandler,
) http.Handler {
	return &modifyingHandler{
		next:          next,
		originalFs:    originalFs,
		targetConfigs: targetConfigs,
		dataCache:     dataCache,
		headerCache:   headerCache,
		logger:        logger,
		errHandler:    errHandler,
		// pendingLocks initialized implicitly by sync.Map
	}
}

func fileMatcherToRxString(fileMatcher string) string {
	var builder strings.Builder
	builder.Grow(3 * len(fileMatcher))
	for _, char := range fileMatcher {
		builder.WriteString("[")
		builder.WriteString(string(char))
		builder.WriteString("]")
	}
	result := builder.String()
	result = strings.ReplaceAll(result, "[*][*][/]", "(.+?/)*")
	result = strings.ReplaceAll(result, "[*][*]", ".*?")
	result = strings.ReplaceAll(result, "[*]", "[^/]*?")
	return result
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

	hasTargetConfig := false
	shouldCache := true
	matchingModifiers := make([]FileModifier, 0)
	for _, targetConfig := range h.targetConfigs {
		fileMatcherAsRxString := fileMatcherToRxString(targetConfig.TargetFile)
		fileMatcherRx := regexp.MustCompile(fileMatcherAsRxString)
		if fileMatcherRx.Match([]byte(fsPath)) {
			hasTargetConfig = true
			matchingModifiers = append(matchingModifiers, targetConfig.Modifier)
			if !targetConfig.CacheResult {
				shouldCache = false
			}
		}
	}

	// If not a targeted file, delegate to the next handler (http.FileServer)
	if !hasTargetConfig {
		h.next.ServeHTTP(w, r)
		return
	}

	var modifier FileModifier = NewCompositeModifier(matchingModifiers...)

	h.logger.LogAttrs(ctx, slog.LevelDebug, "Targeted file requested", slog.String("path", fsPath))

	// Check cache first
	if shouldCache {
		cachedData, cachedDataFound := h.dataCache.Get(fsPath)
		cachedHeaders, cachedHeadersFound := h.headerCache.Get(fsPath)
		if cachedDataFound && cachedHeadersFound {
			h.logger.LogAttrs(ctx, slog.LevelDebug, "Serving modified file from cache", slog.String("path", fsPath))
			serveContent(w, r, fsPath, *cachedData, *cachedHeaders)
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
	if shouldCache {
		cachedData, cachedDataFound := h.dataCache.Get(fsPath)
		cachedHeaders, cachedHeadersFound := h.headerCache.Get(fsPath)
		if cachedDataFound && cachedHeadersFound {
			h.logger.LogAttrs(ctx, slog.LevelDebug, "Serving modified file from cache (post-lock)", slog.String("path", fsPath))
			serveContent(w, r, fsPath, *cachedData, *cachedHeaders)
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

	dataToServe := originalData
	headersToServe := w.Header()
	fileModifierContext := FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: headersToServe.Clone(),
			Path:    fsPath,
		},
		Scratch: make(map[string]any),
	}

	fileContentModifier, isFileContentModifier := modifier.(FileContentModifier)
	if isFileContentModifier {
		modifiedData, err := fileContentModifier.ModifyContent(fileModifierContext, originalData)
		if err != nil {
			h.logger.LogAttrs(ctx, slog.LevelError, "File content modification failed", slog.String("path", fsPath), slog.Any("error", err))
			h.errHandler(http.StatusInternalServerError, w, r)
			return
		}

		dataToServe = modifiedData
	}

	// Determine Content-Type
	contentType := mime.TypeByExtension(path.Ext(fsPath))
	if contentType == "" {
		contentType = http.DetectContentType(dataToServe)
	}
	w.Header().Set("Content-Type", contentType)

	fileResponseHeaderModifier, isFileResponseHeaderModifier := modifier.(FileResponseHeaderModifier)
	if isFileResponseHeaderModifier {
		modifiedHeaders, err := fileResponseHeaderModifier.ModifyResponseHeaders(fileModifierContext)
		if err != nil {
			h.logger.LogAttrs(ctx, slog.LevelError, "File header modification failed", slog.String("path", fsPath), slog.Any("error", err))
			h.errHandler(http.StatusInternalServerError, w, r)
			return
		}

		modifiedHeadersClone := modifiedHeaders.Clone()

		fileModifierContext.Request.Headers = modifiedHeadersClone
		headersToServe = modifiedHeadersClone
	}

	// Cache if configured
	if shouldCache {
		h.dataCache.Set(fsPath, &dataToServe)
		h.headerCache.Set(fsPath, &headersToServe)
		h.logger.LogAttrs(ctx, slog.LevelDebug, "Stored modifications in cache", slog.String("path", fsPath))
	}

	h.pendingLocks.Delete(fsPath)

	// Serve the modified content
	serveContent(w, r, fsPath, dataToServe, headersToServe)
}

// serveContent writes the data to the response writer, setting Content-Type.
func serveContent(w http.ResponseWriter, r *http.Request, filePath string, data []byte, headers http.Header) {
	for headerName := range w.Header().Clone() {
		w.Header().Del(headerName)
	}
	for headerName, headerValues := range headers {
		for _, headerValue := range headerValues {
			w.Header().Set(headerName, headerValue)
		}
	}
	// Serve using http.ServeContent for range requests etc? Requires io.ReadSeeker.
	// Simpler: just write the bytes.
	// For more robust serving (range requests, etc.), wrap bytes in a reader seeker:
	http.ServeContent(w, r, path.Base(filePath), time.Time{}, bytes.NewReader(data))
	// w.Header().Set("Content-Length", strconv.Itoa(len(data))) // Set by ServeContent
	// w.WriteHeader(http.StatusOK) // Set by ServeContent
	// w.Write(data) // Handled by ServeContent
}
