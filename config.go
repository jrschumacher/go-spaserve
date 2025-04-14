package spaserve

import (
	"context" // Added back context for logger
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path" // Use path for FS paths
	"regexp"
	"strings"
)

// TargetConfig defines how a specific file path should be handled.
type TargetConfig struct {
	// Path within the fs.FS to target for modification (e.g., "index.html", "assets/config.js").
	// This path should be relative to the FS root and use forward slashes.
	TargetFile string

	// The modifier implementation to apply to this file.
	Modifier FileModifier

	// If true, the result of Modifier.Modify will be stored in the cache.
	// Set to false if the modification is dynamic or should always re-run.
	CacheResult bool
}

// SpaServerConfig holds the overall configuration for the SPA server handler.
type SpaServerConfig struct {
	// The source filesystem containing the static assets. (Required)
	FS fs.FS

	// List of file modification rules. (Optional)
	Targets []TargetConfig

	// The path (relative to FS root) to serve when a requested path
	// doesn't correspond to an existing file and appears to be an SPA route
	// (i.e., doesn't have a file extension). Defaults to "index.html".
	SpaFallbackPath string

	// The base path prefix for server requests (e.g., "/app"). Requests must
	// start with this path. The prefix is stripped before looking up files
	// in the FS. Defaults to "/". Must start and end with '/'.
	BasePath string

	// Optional logger. Defaults to slog.Default().
	Logger *slog.Logger

	// Optional custom HTTP error handler function. It's given the status code
	// and should write the error response. Defaults to a basic http.Error response.
	MuxErrorHandler func(statusCode int, w http.ResponseWriter, r *http.Request)
}

// Valid JS identifier regex (simplistic, allows leading underscore/dollar)
var identifierRegex = regexp.MustCompile(`^[a-zA-Z_$][a-zA-Z0-9_$]*$`)

// defaultErrorHandler provides the default implementation for handling errors.
func defaultErrorHandler(statusCode int, w http.ResponseWriter, r *http.Request) {
	http.Error(w, http.StatusText(statusCode), statusCode)
}

// configure applies defaults and validates the SpaServerConfig.
func (c *SpaServerConfig) configure() error {
	if c.FS == nil {
		return ErrMissingFS
	}

	if c.SpaFallbackPath == "" {
		c.SpaFallbackPath = "index.html"
	}
	// Ensure SpaFallbackPath is clean and relative
	c.SpaFallbackPath = strings.TrimPrefix(path.Clean("/"+c.SpaFallbackPath), "/") // Ensure relative path

	if c.BasePath == "" {
		c.BasePath = "/"
	}
	if !strings.HasPrefix(c.BasePath, "/") {
		return ErrInvalidBasePath
	}
	// Ensure trailing slash for prefix trimming logic
	if !strings.HasSuffix(c.BasePath, "/") && c.BasePath != "/" {
		c.BasePath = c.BasePath + "/"
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	if c.MuxErrorHandler == nil {
		c.MuxErrorHandler = defaultErrorHandler
	}

	for i, t := range c.Targets {
		if t.Modifier == nil || t.TargetFile == "" {
			return ErrConfigTargetMissing
		}
		// Clean the target path, ensure relative
		cleanedTarget := strings.TrimPrefix(path.Clean("/"+t.TargetFile), "/")
		c.Targets[i].TargetFile = cleanedTarget
	}

	return nil
}

// newInternalErrorHandler wraps the user's MuxErrorHandler.
func newInternalErrorHandler(handler func(int, http.ResponseWriter, *http.Request)) internalErrorHandler {
	if handler == nil {
		handler = defaultErrorHandler // Ensure it's never nil
	}
	return func(statusCode int, w http.ResponseWriter, r *http.Request) {
		handler(statusCode, w, r)
	}
}

// --- Logger Implementation ---

// slogLogger wraps slog.Logger to implement internalLogger.
type slogLogger struct {
	logger *slog.Logger
}

func newInternalLogger(logger *slog.Logger) internalLogger {
	if logger == nil {
		// Provide a default logger that logs errors to stderr
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &slogLogger{logger: logger}
}

func (l *slogLogger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	// Check if logger is enabled for the level before logging
	if l.logger.Enabled(ctx, level) {
		l.logger.LogAttrs(ctx, level, msg, attrs...)
	}
}
