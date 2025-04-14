package spaserve

import (
	"fmt"
	"io/fs"
	"maps" // Requires Go 1.21+
	"net/http"
)

type spaServerOption func(SpaServerConfig) SpaServerConfig

func WithHtmlPageWhitelist(whitelist []string) spaServerOption {
	return func(c SpaServerConfig) SpaServerConfig {
		c.HtmlPageWhitelist = whitelist
		return c
	}
}

func WithSpaFallbackPath(spaFallbackPath string) spaServerOption {
	return func(c SpaServerConfig) SpaServerConfig {
		c.SpaFallbackPath = spaFallbackPath
		return c
	}
}

func WithTargets(targets []TargetConfig) spaServerOption {
	return func(c SpaServerConfig) SpaServerConfig {
		c.Targets = targets
		return c
	}
}

// NewSpaServer creates a new http.Handler configured to serve a Single Page Application.
// It applies file modifications, handles SPA routing fallbacks, and serves static files.
func NewSpaServer(filesys fs.FS, fn ...interface{}) (http.Handler, error) {
	config := SpaServerConfig{
		FS:              filesys,
		SpaFallbackPath: "/",
	}
	staticFileServerHandlerOpts := defaultStaticFilesHandlerOpts
	for _, f := range fn {
		spaServerOption, isSPAServerOption := f.(spaServerOption)
		if isSPAServerOption {
			config = spaServerOption(config)
		}
		staticFileServerOption, isStaticFileServerOption := f.(staticFilesHandlerFunc)
		if isStaticFileServerOption {
			staticFileServerHandlerOpts = staticFileServerOption(staticFileServerHandlerOpts)
		}
		if !isSPAServerOption && !isStaticFileServerOption {
			panic(fmt.Errorf("Unknown option: %q", f))
		}
	}
	// Updates from backwards-compatible flags
	config.BasePath = staticFileServerHandlerOpts.basePath
	config.Logger = staticFileServerHandlerOpts.logger
	config.MuxErrorHandler = newMuxErrorHandler(staticFileServerHandlerOpts.muxErrHandler)

	// 1. Apply defaults and validate configuration
	err := config.configure()
	if err != nil {
		return nil, err
	}

	// 2. Prepare dependencies
	logger := newInternalLogger(config.Logger)
	errorHandler := newInternalErrorHandler(config.MuxErrorHandler)
	checker := NewFSFileChecker(config.FS)
	cache := NewMemoryCache() // Always create a memory cache for now

	// Create a map for efficient target lookup
	targetsMap := make(map[string]TargetConfig, len(config.Targets))
	for _, t := range config.Targets {
		// Use the cleaned path from config.configure()
		targetsMap[t.TargetFile] = t
	}
	// Make map immutable for handlers? Not strictly necessary but good practice.
	// If Go >= 1.21 use maps.Clone
	targetsMap = maps.Clone(targetsMap)

	// 3. Assemble the middleware chain (order matters: outer handlers run first)

	// Base handler: Serves files directly from the original FS.
	// This is the innermost handler
	server := http.FileServer(http.FS(config.FS))

	// Check that the requested HTML file is allowed
	server = newHtmlPageWhitelistHandler(server, config.HtmlPageWhitelist, logger)

	// Index page handler makes sure we fetch index pages properly.
	server = newIndexPageHandler(server)

	// Backwards-compatible modifier handler: if using opts.ns and opts.webEnv, add the modifier
	// This replicates the staticFileServerHandler behavior
	if staticFileServerHandlerOpts.ns != "" && staticFileServerHandlerOpts.webEnv != nil {
		envModifier, err := CreateHtmlScriptTagEnvModifier(
			staticFileServerHandlerOpts.webEnv,
			staticFileServerHandlerOpts.ns,
		)
		if err != nil {
			return nil, err
		}
		backCompatTargetsMap := map[string]TargetConfig{
			"index.html": {
				TargetFile:  "index.html",
				Modifier:    envModifier,
				CacheResult: true,
			},
		}
		server = newModifyingHandler(server, config.FS, backCompatTargetsMap, cache, logger, errorHandler)
	}

	// Modifier handler: Intercepts requests for targeted files, modifies them (using cache).
	// Delegates non-targeted requests or serves modified content.
	server = newModifyingHandler(server, config.FS, targetsMap, cache, logger, errorHandler)

	// SPA Router handler: Rewrites requests for non-existent paths without extensions
	// to the SpaFallbackPath. Delegates all other requests.
	server = newSpaRouterHandler(server, checker, config.SpaFallbackPath, logger)

	// Base Path handler: Strips the configured BasePath prefix from requests.
	// This is the outermost handler.
	server = newBasePathHandler(server, config.BasePath)

	return server, nil
}

// Helper function to easily create the common HTML script modifier.
// Returns the modifier and nil error if successful, otherwise nil modifier and error.
func CreateHtmlScriptTagEnvModifier(env any, namespace string) (FileModifier, error) {
	return NewHtmlScriptTagEnvModifier(env, namespace)
}

// Helper function to easily create a composite modifier.
func CreateCompositeModifier(modifiers ...FileModifier) FileModifier {
	return NewCompositeModifier(modifiers...)
}
