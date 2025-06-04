# Go SPA Serve

[![Go Report Card](https://goreportcard.com/badge/github.com/jrschumacher/go-spaserve)](https://goreportcard.com/report/github.com/jrschumacher/go-spaserve)
[![codecov](https://codecov.io/gh/jrschumacher/go-spaserve/graph/badge.svg?token=W99WAK10IX)](https://codecov.io/gh/jrschumacher/go-spaserve)
[![Go Reference](https://pkg.go.dev/badge/github.com/jrschumacher/go-spaserve.svg)](https://pkg.go.dev/github.com/jrschumacher/go-spaserve)

**`go-spaserve` is a flexible Go package designed to serve static files and Single Page Applications (SPAs) with capabilities for runtime file modifications, such as injecting environment variables.**

It provides an `http.Handler` that intelligently serves files from a given `io/fs.FS`, handles SPA routing by serving a fallback file (like `index.html`) for unknown paths, allows for targeted modifications to specific files before they are served, and includes options for whitelisting accessible HTML pages. Modifications are performed lazily (on first request) and can be cached for performance.

## Motivation

Modern web development often involves build tools (like Vite, Webpack, etc.) that bundle application assets. While excellent for development and optimization, they often bake in build-time configurations. This presents challenges when:

1.  Building a single container image intended for multiple deployment environments (dev, staging, prod) with different runtime configurations (e.g., API endpoints).
2.  Deploying applications on-premises where environment details aren't known until deployment time.

`go-spaserve` addresses this by allowing you to serve your pre-built static assets while enabling targeted, server-side modifications at runtime. The most common use case is injecting runtime environment variables directly into your `index.html` or configuration JavaScript files *after* the application has been built.

## Features

*   **SPA Routing:** Correctly serves a fallback HTML file (e.g., `index.html`) for paths that don't match static files and don't have extensions, allowing client-side routers to take over.
*   **Static File Serving:** Efficiently serves other static assets (JS, CSS, images) from any `io/fs.FS` (including `embed.FS` and `os.DirFS`).
*   **Runtime File Modification:** Define specific files (`TargetFile`) within the `fs.FS` to be modified *before* serving using custom `FileModifier` implementations via the `WithTargets` option.
*   **HTML Script Injection:** Includes a built-in `HtmlScriptTagEnvModifier` to easily inject Go data structures (as JSON) into HTML `<head>` sections (e.g., `window.APP_CONFIG = {...};`). Can be configured via `WithTargets` or the legacy `WithInjectWebEnv` option.
*   **Content Security Policy (CSP) Support:**
    *   `CSPResponseHeaderModifier`: Adds or extends Content-Security-Policy headers in HTTP responses.
    *   `CSPContentNonceModifier`: Injects cryptographically secure nonces into `<script>`, `<style>`, and `<link rel="stylesheet">` tags, enabling strict CSP modes. It also handles inline `style` attributes by converting them into nonce-protected `<style>` tags.
*   **Composable Modifiers:** Chain multiple `FileContentModifier` and `FileResponseHeaderModifier` implementations together for a single file using the `CompositeModifier` (used with `WithTargets`).
*   **Configurable Caching:** Modified files can be automatically cached in memory to avoid reprocessing on subsequent requests (controlled within `TargetConfig`).
*   **Base Path Handling:** Serve the entire application under a specific URL prefix (e.g., `/myapp/`) using the `WithBasePath` option.
*   **HTML Page Whitelisting:** Restrict access to only specified HTML files using `WithHtmlPageWhitelist`.
*   **Customizable Logging:** Integrates with `log/slog` using the `WithLogger` option.
*   **Customizable Error Handling:** Provide your own handler for HTTP errors (404, 500) using the `WithMuxErrorHandler` option.
*   **Interface-Based:** Core components like `FileModifier`, `FileContentModifier`, `FileResponseHeaderModifier`, `Cache`, and `FileChecker` are interface-based for testability and extensibility.

## Installation

```bash
go get github.com/jrschumacher/go-spaserve
```

## Usage

The primary entrypoint is `spaserve.NewSpaServer(fsys, options...)`, which takes an `io/fs.FS` and a variable number of functional options. These options configure various aspects of the server behavior.

```go
// Example signature:
handler, err := spaserve.NewSpaServer(
    myFileSystem,
    spaserve.WithBasePath("/app/"),
    spaserve.WithSpaFallbackPath("entrypoint.html"),
    spaserve.WithTargets([]spaserve.TargetConfig{ /* ... */ }),
    spaserve.WithHtmlPageWhitelist([]string{"entrypoint.html", "about.html"}),
    spaserve.WithLogger(myLogger),
    // ... other options
)
```

### Example 1: Basic SPA Server (No Modifications)

This example serves an embedded filesystem, falling back to `index.html` for unknown paths.

```go
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/jrschumacher/go-spaserve"
)

//go:embed dist/*
var embeddedFiles embed.FS

func main() {
	distFS, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatal("Failed to get sub filesystem:", err)
	}

	// Configure the SPA server using functional options
	// No options needed for basic SPA serving with defaults
	// (Fallback: "index.html", BasePath: "/")
	spaHandler, err := spaserve.NewSpaServer(distFS)
	if err != nil {
		log.Fatal("Failed to create SPA handler:", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler) // Handle all requests

	log.Println("Starting server on :8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
```

### Example 2: SPA with Runtime Environment Injection (Recommended Way)

Injects a Go struct into `index.html` as `window.APP_CONFIG` using `WithTargets` and the built-in `CreateHtmlScriptTagEnvModifier`.

```go
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os" // For reading env vars

	"github.com/jrschumacher/go-spaserve"
)

//go:embed dist/*
var embeddedFiles embed.FS

// Define the structure for your frontend configuration
type AppConfig struct {
	ApiEndpoint string `json:"apiEndpoint"`
	FeatureFlag bool   `json:"featureFlag"`
	Theme       string `json:"theme"`
}

func main() {
	distFS, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatal("Failed to get sub filesystem:", err)
	}

	appEnv := AppConfig{
		ApiEndpoint: os.Getenv("API_ENDPOINT"),
		FeatureFlag: os.Getenv("ENABLE_FEATURE_X") == "true",
		Theme:       "dark",
	}

	// Create the HTML script modifier
	htmlModifier, err := spaserve.CreateHtmlScriptTagEnvModifier(appEnv, "APP_CONFIG")
	if err != nil {
		log.Fatalf("Failed to create HTML modifier: %v", err)
	}

	// Define the target configuration
	targets := []spaserve.TargetConfig{
		{
			TargetFile:  "index.html", // Specify file relative to FS root
			Modifier:    htmlModifier,
			CacheResult: true,
		},
	}

	// Configure the SPA server using functional options
	spaHandler, err := spaserve.NewSpaServer(
		distFS,
		spaserve.WithTargets(targets), // Use WithTargets for modifications
	)
	if err != nil {
		log.Fatal("Failed to create SPA handler:", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler)

	log.Println("Starting server with env injection on :8080...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
```

### Example 3: Composite Modifications

Apply multiple content or header modifications to the same file using `WithTargets` and `CreateCompositeModifier`.

```go
package main

import (
	// ... other imports from Example 2
	"github.com/jrschumacher/go-spaserve"
)

// ... embed, AppConfig struct etc.

func main() {
	distFS, err := fs.Sub(embeddedFiles, "dist")
	// ... handle error

	appEnv := AppConfig{ /* ... populate ... */ }
	userSession := map[string]string{"userId": "user-123", "role": "admin"}

	modifier1, err1 := spaserve.CreateHtmlScriptTagEnvModifier(appEnv, "APP_CONFIG")
	modifier2, err2 := spaserve.CreateHtmlScriptTagEnvModifier(userSession, "USER_SESSION")
	if err1 != nil || err2 != nil {
		log.Fatalf("Failed to create modifiers: %v, %v", err1, err2)
	}

	// Combine them using a CompositeModifier
	compositeModifier := spaserve.CreateCompositeModifier(modifier1, modifier2)

	targets := []spaserve.TargetConfig{
		{
			TargetFile:  "index.html",
			Modifier:    compositeModifier, // Use the composite modifier
			CacheResult: true,
		},
	}

	spaHandler, err := spaserve.NewSpaServer(
		distFS,
		spaserve.WithTargets(targets),
	)
	// ... handle error, start server ...
}

```

### Example 4: Serving from OS Filesystem with Base Path & Whitelist

```go
package main

import (
	"log"
	"net/http"
	"os" // Use os.DirFS

	"github.com/jrschumacher/go-spaserve"
)

func main() {
	osFS := os.DirFS("./static-assets")

	// Configure the SPA server
	basePath := "/myapp/"
	spaHandler, err := spaserve.NewSpaServer(
		osFS,
		spaserve.WithBasePath(basePath), // Serve under /myapp/
		// Only allow access to these specific HTML files directly
		spaserve.WithHtmlPageWhitelist([]string{"index.html", "login.html"}),
		spaserve.WithSpaFallbackPath("index.html"), // Fallback for /myapp/unknown routes
	)
	if err != nil {
		log.Fatal("Failed to create SPA handler:", err)
	}

	mux := http.NewServeMux()
	// IMPORTANT: The pattern here must match the BasePath!
	mux.Handle(basePath, spaHandler)

	log.Println("Starting OS FS server on :8080 under /myapp/ ...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
```

### Example 5: Advanced: CSP Nonce Injection

This example demonstrates injecting CSP nonces into `script`, `style`, and `link rel="stylesheet"` tags, handling inline styles, and setting the `Content-Security-Policy` header.

```go
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/jrschumacher/go-spaserve"
)

//go:embed dist/*
var embeddedFiles embed.FS

// AppConfig structure (re-used for consistency)
type AppConfig struct {
	ApiEndpoint string `json:"apiEndpoint"`
	FeatureFlag bool   `json:"featureFlag"`
}

func main() {
	distFS, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatal("Failed to get sub filesystem:", err)
	}

	appEnv := AppConfig{
		ApiEndpoint: os.Getenv("API_ENDPOINT"),
		FeatureFlag: os.Getenv("ENABLE_FEATURE_X") == "true",
	}

	// 1. Create the CSP content nonce modifier.
	// This modifier handles:
	//   - Adding 'nonce' attributes to HTML elements (script, style, link rel=stylesheet).
	//   - Converting inline 'style' attributes to nonce-protected <style> blocks.
	//   - Storing the generated nonce in the `FileModifierContext.Scratch` map.
	//   - Automatically adding 'nonce-...' sources to `script-src` and `style-src` directives
	//     in the Content-Security-Policy header.
	nonceContentModifier := spaserve.NewCSPContentNonceModifier(spaserve.CSPContentNonceModifierOptions{
		NonceLength:             16, // 16 bytes for a 32-character hexadecimal nonce
		NonceStringReplacements: []string{"__CSP_NONCE__"}, // Optional: replace this placeholder if it exists in your HTML/JS
	})

	// 2. Create a custom CSP header modifier for *other* directives.
	// This modifier is for directives *not* directly related to nonces,
	// or for overriding defaults. It should *not* manually add nonce sources.
	// The `CSPContentNonceModifier`'s `ModifyResponseHeaders` method, when executed
	// by the `CompositeModifier`, will merge its nonce-related directives with these.
	customCSPModifier := spaserve.NewCSPResponseHeaderModifier(func(context spaserve.FileModifierContext) (string, error) {
		// Define your base CSP directives here.
		// Example: Allow images from self and data URIs, allow websocket connections
		return "default-src 'self'; img-src 'self' data:; connect-src 'self' ws:;", nil
	})

	// 3. (Optional) Create an environment injection modifier (from Example 2)
	htmlEnvModifier, err := spaserve.CreateHtmlScriptTagEnvModifier(appEnv, "APP_CONFIG")
	if err != nil {
		log.Fatalf("Failed to create HTML env modifier: %v", err)
	}

	// 4. Combine all modifiers using a CompositeModifier.
	// The order is important:
	// - htmlEnvModifier (FileContentModifier): Injects app config into HTML.
	// - nonceContentModifier (FileContentModifier & FileResponseHeaderModifier):
	//   - Its ModifyContent runs *first* to process HTML, add nonces, convert inline styles, and store nonce in scratch.
	//   - Its ModifyResponseHeaders runs *later* (with other header modifiers) to add nonce sources to CSP header.
	// - customCSPModifier (FileResponseHeaderModifier): Its ModifyResponseHeaders runs *later* to add other base CSP directives.
	// The `CompositeModifier`'s header merging logic ensures all directives are correctly combined.
	compositeCSPModifier := spaserve.CreateCompositeModifier(
		htmlEnvModifier,       // Content: Injects app config
		nonceContentModifier,  // Content: Adds nonces to elements, converts inline styles, stores nonce in scratch.
		customCSPModifier,     // Header: Adds other user-defined CSP directives. (Nonce sources are added by nonceContentModifier)
	)

	// Define the target configuration for `index.html`
	targets := []spaserve.TargetConfig{
		{
			TargetFile:  "index.html", // Apply all these modifiers to index.html
			Modifier:    compositeCSPModifier,
			CacheResult: false,         // Do not cache CSP updates
		},
	}

	// Configure the SPA server
	spaHandler, err := spaserve.NewSpaServer(
		distFS,
		spaserve.WithTargets(targets),
		spaserve.WithSpaFallbackPath("index.html"),
	)
	if err != nil {
		log.Fatal("Failed to create SPA handler:", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler)

	log.Println("Starting server with CSP nonce and env injection on :8080...")
	// For this example to work, your `dist/index.html` might look like:
	// <html>
	// <head>
	//     <title>CSP Test</title>
	//     <script>console.log('inline script');</script>
	//     <link rel="stylesheet" href="/assets/style.css">
	//     <style>body { margin: 0; }</style>
	// </head>
	// <body>
	//     <p style="color:red;">Inline style text (will be converted)</p>
	//     <script src="/app.js"></script>
	//     <div id="app"></div>
	//     <p>Nonce placeholder: __CSP_NONCE__ (will be replaced)</p>
	// </body>
	// </html>
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
```

## Configuration Options for `NewSpaServer`

The `NewSpaServer(fsys, options...)` function accepts the following functional options:

*   **`WithTargets(targets []TargetConfig)`**: (Recommended for modifications) Defines files to be modified before serving. `TargetConfig` includes:
    *   `TargetFile string`: Path relative to the FS root.
    *   `Modifier FileModifier`: Implementation to transform content (e.g., `CreateHtmlScriptTagEnvModifier`).
    *   `CacheResult bool`: Whether to cache the modified result.
*   **`WithHtmlPageWhitelist(whitelist []string)`**: Restricts direct access to only the listed HTML files (paths relative to FS root, e.g., `"index.html"`, `"about/index.html"`). If not provided, all HTML files are accessible.
*   **`WithSpaFallbackPath(path string)`**: Sets the relative path within the FS to serve for SPA routes (requests for non-file paths without extensions). Defaults to `"index.html"`.

Additionally, `NewSpaServer` accepts the following options, which historically originated from the `NewStaticFilesHandler` (older API) but are fully compatible and functional within `NewSpaServer`:

*   **`WithBasePath(path string)`**: Sets the URL prefix for the server. All incoming requests must start with this path. The prefix is then stripped before file lookup in the `fs.FS`. Must start and end with `/` unless it's just `/`. Defaults to `/`.
*   **`WithLogger(logger *slog.Logger)`**: Provides a custom `slog.Logger` instance for internal logging. Defaults to `slog.Default()`.
*   **`WithInjectWebEnv(env any, namespace string)`**: *(Legacy/Convenience)* Injects the given `env` data (as JSON) into `index.html` (and *only* `index.html`) under the specified `namespace`. If `index.html` is not found, an error is returned. Default namespace is `"APP_ENV"`. For more general and flexible file modifications, including `index.html`, prefer using `WithTargets` with `CreateHtmlScriptTagEnvModifier`.
*   **`WithMuxErrorHandler(handler func(int) http.Handler)`**: Provides a custom HTTP error handler. The function takes a status code and should return an `http.Handler` to handle the error response. Defaults to a basic `http.Error` response.

*Note on Option Types:* `NewSpaServer` accepts `...interface{}` to allow for a flexible combination of both `spaServerOption` and `staticFilesHandlerFunc` types. This design ensures backward compatibility while promoting a more powerful and extensible modification system via `WithTargets`.

## Extensibility (`FileModifier` Interface)

You can create custom file modifications by implementing the relevant interfaces:

```go
// FileModifier is an empty interface, acting as a marker interface.
// Concrete modification logic is defined in FileContentModifier and FileResponseHeaderModifier.
type FileModifier interface{}

// FileModifierContext provides context for file modification operations.
type FileModifierContext struct {
	Request FileModifierContextRequest // Contains details about the HTTP request.
	Scratch map[string]any             // A map for modifiers to store/retrieve transient data during a request.
}

// FileModifierContextRequest contains request-specific data for modifiers.
type FileModifierContextRequest struct {
	Context context.Context // The request's context.
	Headers http.Header     // A mutable copy of the response headers for the current request.
	Path    string          // The file path being modified (relative to the FS root).
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
```

**Example: Placeholder Replacer**

```go
package main

import (
	"bytes"
	"fmt"
	"net/http" // For http.Header

	"github.com/jrschumacher/go-spaserve"
)

type PlaceholderModifier struct {
	Placeholder string
	Value       string
}

func (pm *PlaceholderModifier) ModifyContent(context spaserve.FileModifierContext, originalContent []byte) ([]byte, error) {
	modified := bytes.ReplaceAll(originalContent, []byte(pm.Placeholder), []byte(pm.Value))
	if bytes.Equal(modified, originalContent) {
		fmt.Printf("Warning: Placeholder %q not found in file %q\n", pm.Placeholder, context.Request.Path)
	}
	return modified, nil
}

// PlaceholderModifier does not modify headers, so it can have a no-op implementation
func (pm *PlaceholderModifier) ModifyResponseHeaders(context spaserve.FileModifierContext) (http.Header, error) {
	return context.Request.Headers, nil
}

// Usage with WithTargets:
// versionModifier := &PlaceholderModifier{ Placeholder: "__APP_VERSION__", Value: "1.2.3" }
// targets := []spaserve.TargetConfig {
//   { TargetFile: "main.js", Modifier: versionModifier, CacheResult: true },
// }
// handler, err := spaserve.NewSpaServer(fsys, spaserve.WithTargets(targets))
```
This allows for various transformations like replacing placeholders, modifying CSS variables, processing template files, etc., all performed server-side at runtime.

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
