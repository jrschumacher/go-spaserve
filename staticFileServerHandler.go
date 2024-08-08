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
	logger        *slog.Logger
	muxErrHandler func(int) http.Handler
	webEnv        any
}

type staticFilesHandlerFunc func(staticFilesHandlerOpts) staticFilesHandlerOpts

var defaultStaticFilesHandlerOpts = staticFilesHandlerOpts{
	ns:            "APP_ENV",
	basePath:      "/",
	logger:        nil,
	muxErrHandler: nil,
	webEnv:        nil,
}

// WithLogger sets the logger for the static file server. Defaults to slog.Logger.
func WithLogger(logger *slog.Logger) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.logger = logger
		return c
	}
}

// WithBasePath sets the base path for the web server which will be trimmed from the request path before looking up files.
func WithBasePath(basePath string) staticFilesHandlerFunc {
	if basePath == "" {
		basePath = defaultStaticFilesHandlerOpts.basePath
	}

	// ensure leading slash for trimming later
	if basePath[0] != '/' {
		basePath = "/" + basePath
	}

	// ensure trailing slash for trimming later
	if basePath[len(basePath)-1] != '/' {
		basePath = basePath + "/"
	}

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
	if namespace == "" {
		namespace = defaultStaticFilesHandlerOpts.ns
	}

	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.webEnv = env
		c.ns = namespace
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
	opts := defaultStaticFilesHandlerOpts
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

		// warn if base path might be wrong
		if len(opts.basePath) > 0 && r.URL.Path[:len(opts.basePath)] != opts.basePath {
			logWithAttrs(slog.LevelInfo, "WARNING: base path may not be set correctly",
				slog.Attr{Key: "reqPath", Value: slog.StringValue(r.URL.Path)},
				slog.Attr{Key: "basePath", Value: slog.StringValue(opts.basePath)},
			)
		}

		// clean path for security and consistency
		cleanedPath := path.Clean(r.URL.Path)
		cleanedPath = strings.TrimPrefix(cleanedPath, opts.basePath)

		// open file
		file, err := mfilesys.Open(cleanedPath)
		if file != nil {
			defer file.Close()
		}

		// ensure leading slash
		r.URL.Path = cleanedPath
		if r.URL.Path[0] != '/' {
			r.URL.Path = "/" + r.URL.Path
		}

		// if index.html is requested, rewrite to avoid 301 redirect
		if r.URL.Path == "/index.html" {
			r.URL.Path = "/"
		}

		isErrNotExist := errors.Is(err, os.ErrNotExist)
		isFile := path.Ext(cleanedPath) != ""

		// if err != nil {
		// 	fmt.Printf("error: %v\n", err)
		// 	fmt.Printf("request path: %s\n", r.URL.Path)
		// 	fmt.Printf("cleaned path: %s\n", cleanedPath)
		// 	fs.WalkDir(mfilesys, ".", func(path string, d fs.DirEntry, err error) error {
		// 		fmt.Printf("path: %s, d: %v, err: %v\n", path, d, err)
		// 		return nil
		// 	})
		// }

		// return 500 for other errors
		if err != nil && !isErrNotExist {
			logWithAttrs(slog.LevelError, "could not open file", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			muxErrHandler(http.StatusInternalServerError, w, r)
			return
		}

		// return 404 for actual static file requests that don't exist
		if err != nil && isErrNotExist && isFile {
			logWithAttrs(slog.LevelError, "could not find file", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			muxErrHandler(http.StatusNotFound, w, r)
			return
		}

		// serve index.html and let SPA handle undefined routes
		if isErrNotExist {
			logWithAttrs(slog.LevelDebug, "not found, serve index", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			r.URL.Path = "/"
		}

		fileServer.ServeHTTP(w, r)
	}), nil
}

// newLoggerWithContext creates a new logger function with the given context and logger.
func newLoggerWithContext(ctx context.Context, logger *slog.Logger) func(slog.Level, string, ...slog.Attr) {
	return func(level slog.Level, msg string, attrs ...slog.Attr) {
		if logger == nil {
			return
		}
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
