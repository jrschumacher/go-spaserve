package spaserve

import (
	"context"
	"log/slog"
	"net/http"
)

type FileModifier interface {
	// maybe we should add something here to make it non-empty
}

type FileModifierContextRequest struct {
	Context context.Context
	Headers http.Header
	Path    string
}

type FileModifierContext struct {
	Request FileModifierContextRequest
	Scratch map[string]any
}

// FileContentModifier defines the interface for transforming file content.
type FileContentModifier interface {
	FileModifier // Embeds FileModifier
	// ModifyContent takes the original file path and content,
	// returns the modified content or an error.
	ModifyContent(context FileModifierContext, content []byte) (modifiedContent []byte, err error)
}

// FileResponseHeaderModifier allows HTTP headers to be set for the response of a file.
type FileResponseHeaderModifier interface {
	FileModifier // Embeds FileModifier
	// ModifyResponseHeaders returns a map of HTTP headers that should be applied to the response
	// for the given path and its (potentially modified) content.
	ModifyResponseHeaders(context FileModifierContext) (http.Header, error)
}

// Cache defines the interface for storing and retrieving processed file content.
// Implementations must be safe for concurrent use.
type Cache[T any] interface {
	Get(key string) (data *T, found bool)
	Set(key string, data *T)
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
