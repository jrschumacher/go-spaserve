package pkg

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
)

type staticFilesHandlerOpts struct {
	ns            string
	logger        slog.Logger
	muxErrHandler func(int) http.Handler
}

type staticFilesHandlerFunc func(staticFilesHandlerOpts) staticFilesHandlerOpts

func WithLogger(logger slog.Logger) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.logger = logger
		return c
	}
}

func WithGlobalNamespace(ns string) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.ns = ns
		return c
	}
}

// WithMuxErrorHandler sets custom error handlers for the static file server.
func WithMuxErrorHandler(h func(int) http.Handler) staticFilesHandlerFunc {
	return func(c staticFilesHandlerOpts) staticFilesHandlerOpts {
		c.muxErrHandler = h
		return c
	}
}

func StaticFilesHandler(ctx context.Context, conf any, filesys fs.FS, fn ...staticFilesHandlerFunc) (http.Handler, error) {
	// process options
	opts := staticFilesHandlerOpts{
		ns: defaultGlobalNamespace,
	}
	for _, f := range fn {
		opts = f(opts)
	}

	// inject window vars
	filesys, err := InjectWindowVars(conf, filesys, opts.ns)
	if err != nil {
		return nil, err
	}

	// create file server
	fileServer := http.FileServer(http.FS(filesys))

	// return handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// serve index.html for root path
		if r.URL.Path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		// clean path for security & strip leading slash to avoid FS.Open errors
		cleanedPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		file, err := filesys.Open(cleanedPath)
		errNotExist := errors.Is(err, os.ErrNotExist)
		if errNotExist && path.Ext(cleanedPath) != "" {
			// return 404 for actual static file requests that don't exist
			if opts.logger.Enabled(ctx, slog.LevelError) {
				slog.LogAttrs(ctx, slog.LevelError,
					"could not find file", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)},
				)
			}

			// use custom 404 handler if provided
			if opts.muxErrHandler != nil {
				opts.muxErrHandler(404).ServeHTTP(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		} else if errNotExist {
			// otherwise serve index.html and let SPA handle undefined routes
			if opts.logger.Enabled(ctx, slog.LevelDebug) {
				slog.LogAttrs(ctx, slog.LevelDebug,
					"not found, serve index", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)},
				)
			}

			r.URL.Path = "/"
		} else if err != nil {
			// return 500 for other errors
			if opts.logger.Enabled(ctx, slog.LevelError) {
				slog.LogAttrs(ctx, slog.LevelError,
					"could not open file", slog.Attr{Key: "path", Value: slog.StringValue(cleanedPath)},
				)
			}

			// use custom 500 handler if provided
			if opts.muxErrHandler != nil {
				opts.muxErrHandler(500).ServeHTTP(w, r)
				return
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else {
			// close file if opened
			file.Close()
		}

		// let fileServer handle valid requests
		fileServer.ServeHTTP(w, r)
	}), nil
}
