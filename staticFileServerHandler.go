package spaserve

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/psanford/memfs"
)

type StaticFilesHandler struct {
	opts          staticFilesHandlerOpts
	fileServer    http.Handler
	mfilesys      *memfs.FS
	logger        *servespaLogger
	muxErrHandler func(int, http.ResponseWriter, *http.Request)
}

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
func NewStaticFilesHandler(filesys fs.FS, fn ...staticFilesHandlerFunc) (http.Handler, error) {
	// process options
	opts := defaultStaticFilesHandlerOpts
	for _, f := range fn {
		opts = f(opts)
	}

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
	logger := newLogger(opts.logger)

	return &StaticFilesHandler{
		opts:          opts,
		mfilesys:      mfilesys,
		fileServer:    fileServer,
		logger:        logger,
		muxErrHandler: newMuxErrorHandler(opts.muxErrHandler),
	}, nil
}

func (h *StaticFilesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// clean path for security and consistency
	cleanedPath := path.Clean(r.URL.Path)
	cleanedPath = strings.TrimPrefix(cleanedPath, h.opts.basePath)
	cleanedPath = strings.TrimPrefix(cleanedPath, "/")
	cleanedPath = strings.TrimSuffix(cleanedPath, "/")

	h.logger.logContext(ctx, slog.LevelDebug, "request", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})

	// reconstitute the path
	r.URL.Path = "/" + cleanedPath

	// use root path for index.html
	if r.URL.Path == "index.html" {
		r.URL.Path = "/"
	}

	// handle non-root paths
	if r.URL.Path != "/" {
		// open file
		file, err := h.mfilesys.Open(cleanedPath)
		isErr := err != nil
		isErrNotExist := errors.Is(err, os.ErrNotExist)
		isFile := path.Ext(cleanedPath) != ""
		if file != nil {
			file.Close()
		}

		// return 500 for other errors
		if isErr && !isErrNotExist {
			h.logger.logContext(ctx, slog.LevelError, "could not open file", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			h.muxErrHandler(http.StatusInternalServerError, w, r)
			return
		}

		// return 404 for actual static file requests that don't exist
		if isErrNotExist && isFile {
			h.logger.logContext(ctx, slog.LevelDebug, "not found, static file", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			h.muxErrHandler(http.StatusNotFound, w, r)
			return
		}

		// serve index.html and let SPA handle undefined routes
		if isErrNotExist {
			h.logger.logContext(ctx, slog.LevelDebug, "not found, serve index", slog.Attr{Key: "cleanedPath", Value: slog.StringValue(cleanedPath)})
			r.URL.Path = "/"
		}
	}

	h.fileServer.ServeHTTP(w, r)
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
