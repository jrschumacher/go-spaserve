package spaserve

import (
	"context" // Keep context for logger just in case
	"log/slog"
	"net/http"
)

// FileModifier defines the interface for transforming file content.
type FileModifier interface {
	// Modify takes the original file path and content,
	// returns the modified content or an error.
	Modify(path string, originalContent []byte) (modifiedContent []byte, err error)
}

// Cache defines the interface for storing and retrieving processed file content.
// Implementations must be safe for concurrent use.
type Cache interface {
	Get(key string) (data []byte, found bool)
	Set(key string, data []byte)
	// Consider adding Delete(key string) if cache invalidation becomes necessary.
}

// FileChecker defines the interface for checking file existence.
type FileChecker interface {
	// Exists checks if a file exists at the given path within its associated fs.FS.
	Exists(name string) bool
}

// --- Internal handler/helper interfaces ---

// internalErrorHandler simplifies passing the configured error handler function.
type internalErrorHandler func(statusCode int, w http.ResponseWriter, r *http.Request)

// internalLogger defines simplified logging methods used by handlers.
type internalLogger interface {
	LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr)
}
