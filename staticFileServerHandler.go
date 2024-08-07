package spaserve

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/psanford/memfs"
)

type staticFilesHandlerOpts struct {
	ns            string
	basePath      string
	logger        slog.Logger
	muxErrHandler func(int) http.Handler
	webEnv        any
}

type staticFilesHandlerFunc func(staticFilesHandlerOpts) staticFilesHandlerOpts

const defaultGlobalNamespace = "APP_ENV"

// WithLogger sets the logger for the static file server. Defaults to slog.Logger.
func WithLogger(logger slog.Logger) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.logger = logger
		return c
	}
}

// WithBasePath sets the base path for the web server which will be trimmed from the request path before looking up files.
func WithBasePath(basePath string) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.basePath = basePath
		return c
	}
}

// WithMuxErrorHandler sets custom error handlers for the static file server.
//
//	handler: a function that returns an http.Handler for the given status code
func WithMuxErrorHandler(handler func(int) http.Handler) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.muxErrHandler = handler
		return c
	}
}

// WithInjectWebEnv injects the web environment into the static file server.
//
//	env: the web environment to inject, use json struct tags to drive the marshalling
//	namespace: the namespace to use for the web environment, defaults to "APP_ENV"
func WithInjectWebEnv(env any, namespace string) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.webEnv = env

		if namespace != "" {
			c.ns = namespace
		}
		return c
	}
}

// StaticFilesHandler creates a static file server handler that serves files from the given fs.FS.
// It serves index.html for the root path and 404 for actual static file requests that don't exist.
//   - ctx: the context
//   - filesys: the file system to serve files from - this will be copied to a memfs
//   - fn: optional functions to configure the handler (e.g. WithLogger, WithBasePath, WithMuxErrorHandler, WithInjectWebEnv)
func StaticFilesHandler(ctx context.Context, filesys fs.FS, fn ...staticFilesHandlerFunc) (http.Handler, error) {
	// process options
	opts := staticFilesHandlerOpts{
		ns: defaultGlobalNamespace,
	}
	for _, f := range fn {
		opts = f(opts)
	}

	logWithAttrs := newLoggerWithContext(ctx, opts.logger)
	muxErrHandler := newMuxErrorHandler(opts.muxErrHandler)

	var (
		mfilesys *memfs.FS
		err      error
	)
	// inject web env if provided
	if opts.webEnv != nil {
		mfilesys, err = InjectWebEnv(filesys, opts.webEnv, opts.ns)
	} else {
		mfilesys, err = CopyFileSys(filesys, nil)
	}
	if err != nil {
		return nil, err
	}

	// create file server
	fileServer := http.FileServer(http.FS(mfilesys))

	// return handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// serve index.html for root path
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// clean path for security & strip leading slash to avoid FS.Open errors
		cleanedPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		// open file
		file, err := mfilesys.Open(cleanedPath)
		if file != nil {
			defer file.Close()
		}

		isErrNotExist := errors.Is(err, os.ErrNotExist)
		isFile := path.Ext(cleanedPath) != ""

		// return 500 for other errors
		if err != nil && !isErrNotExist {
			logWithAttrs(slog.LevelError, "could not open file", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)})
			muxErrHandler(http.StatusInternalServerError, w, r)
			return
		}

		// return 404 for actual static file requests that don't exist
		if isErrNotExist && isFile {
			logWithAttrs(slog.LevelError, "could not find file", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)})
			muxErrHandler(http.StatusNotFound, w, r)
			return
		}

		// serve index.html and let SPA handle undefined routes
		if isErrNotExist {
			logWithAttrs(slog.LevelDebug, "not found, serve index", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)})
			r.URL.Path = "/"
		}

		// let fileServer handle valid requests
		fileServer.ServeHTTP(w, r)
	}), nil
}

// newLoggerWithContext creates a new logger function with the given context and logger.
func newLoggerWithContext(ctx context.Context, logger slog.Logger) func(slog.Level, string, ...slog.Attr) {
	return func(level slog.Level, msg string, attrs ...slog.Attr) {
		logger.LogAttrs(ctx, level, msg, attrs...)
	}
}

// newMuxErrorHandler creates a new error handler function with the given muxErrHandler.
func newMuxErrorHandler(muxErrHandler func(int) http.Handler) func(int, http.ResponseWriter, *http.Request) {
	return func(statusCode int, w http.ResponseWriter, r *http.Request) {
		if muxErrHandler != nil {
			muxErrHandler(statusCode).ServeHTTP(w, r)
			return
		}

		http.Error(w, http.StatusText(statusCode), statusCode)
	}
}
