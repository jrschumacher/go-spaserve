package spaserve

import (
	"maps" // Requires Go 1.21+
	"net/http"
)

// NewSpaServer creates a new http.Handler configured to serve a Single Page Application.
// It applies file modifications, handles SPA routing fallbacks, and serves static files.
func NewSpaServer(config SpaServerConfig) (http.Handler, error) {
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
	// This is the innermost handler, only reached for non-modified files.
	baseServer := newIndexPageHandler(http.FileServer(http.FS(config.FS)))

	// Modifier handler: Intercepts requests for targeted files, modifies them (using cache).
	// Delegates non-targeted requests or serves modified content.
	modHandler := newModifyingHandler(baseServer, config.FS, targetsMap, cache, logger, errorHandler)

	// SPA Router handler: Rewrites requests for non-existent paths without extensions
	// to the SpaFallbackPath. Delegates all other requests.
	spaHandler := newSpaRouterHandler(modHandler, checker, config.SpaFallbackPath, logger)

	// Base Path handler: Strips the configured BasePath prefix from requests.
	// This is the outermost handler.
	finalHandler := newBasePathHandler(spaHandler, config.BasePath)

	return finalHandler, nil
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
