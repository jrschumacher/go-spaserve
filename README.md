# Go SPA Serve

[![Go Report Card](https://goreportcard.com/badge/github.com/jrschumacher/go-spaserve)](https://goreportcard.com/report/github.com/jrschumacher/go-spaserve)
[![codecov](https://codecov.io/gh/jrschumacher/go-spaserve/graph/badge.svg?token=W99WAK10IX)](https://codecov.io/gh/jrschumacher/go-spaserve)
[![Go Reference](https://pkg.go.dev/badge/github.com/jrschumacher/go-spaserve.svg)](https://pkg.go.dev/github.com/jrschumacher/go-spaserve)

**`go-spaserve` is a flexible Go package designed to serve static files and Single Page Applications (SPAs) with capabilities for runtime file modifications, such as injecting environment variables.**

It provides an `http.Handler` that intelligently serves files from a given `io/fs.FS`, handles SPA routing by serving a fallback file (like `index.html`) for unknown paths, and allows for targeted modifications to specific files before they are served. Modifications are performed lazily (on first request) and can be cached for performance.

## Motivation

Modern web development often involves build tools (like Vite, Webpack, etc.) that bundle application assets. While excellent for development and optimization, they often bake in build-time configurations. This presents challenges when:

1.  Building a single container image intended for multiple deployment environments (dev, staging, prod) with different runtime configurations (e.g., API endpoints).
2.  Deploying applications on-premises where environment details aren't known until deployment time.

`go-spaserve` addresses this by allowing you to serve your pre-built static assets while enabling targeted, server-side modifications at runtime. The most common use case is injecting runtime environment variables directly into your `index.html` or configuration JavaScript files *after* the application has been built.

## Features

*   **SPA Routing:** Correctly serves a fallback HTML file (e.g., `index.html`) for paths that don't match static files, allowing client-side routers to take over.
*   **Static File Serving:** Efficiently serves other static assets (JS, CSS, images) from any `io/fs.FS` (including `embed.FS` and `os.DirFS`).
*   **Runtime File Modification:** Define specific files (`TargetFile`) within the `fs.FS` to be modified *before* serving using custom `FileModifier` implementations.
*   **HTML Script Injection:** Includes a built-in `HtmlScriptTagEnvModifier` to easily inject Go data structures (as JSON) into HTML `<head>` sections (e.g., `window.APP_CONFIG = {...};`).
*   **Composable Modifiers:** Chain multiple modifications together for a single file using the `CompositeModifier`.
*   **Configurable Caching:** Modified files can be automatically cached in memory to avoid reprocessing on subsequent requests.
*   **Base Path Handling:** Serve the entire application under a specific URL prefix (e.g., `/myapp/`).
*   **Customizable Logging:** Integrates with `log/slog` for structured logging.
*   **Customizable Error Handling:** Provide your own handler for HTTP errors (404, 500).
*   **Interface-Based:** Core components like `FileModifier` and `Cache` are interface-based for testability and extensibility.

## Installation

```bash
go get github.com/jrschumacher/go-spaserve
```

## Usage

The primary entrypoint is `spaserve.NewSpaServer(config)`, which takes a configuration struct and returns an `http.Handler`.

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
	// Get the subdirectory containing the files
	distFS, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		log.Fatal("Failed to get sub filesystem:", err)
	}

	// Configure the SPA server
	config := spaserve.SpaServerConfig{
		FS: distFS,
		// SpaFallbackPath defaults to "index.html"
		// BasePath defaults to "/"
	}

	spaHandler, err := spaserve.NewSpaServer(config)
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

### Example 2: SPA with Runtime Environment Injection

Injects a Go struct into `index.html` as `window.APP_CONFIG`.

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

	// Load runtime configuration (e.g., from environment variables)
	appEnv := AppConfig{
		ApiEndpoint: os.Getenv("API_ENDPOINT"), // Example: Load from env
		FeatureFlag: os.Getenv("ENABLE_FEATURE_X") == "true",
		Theme:       "dark", // Example: Could also come from env
	}

	// Create the HTML script modifier
	// This helper validates the namespace.
	htmlModifier, err := spaserve.NewHtmlScriptTagEnvModifier(appEnv, "APP_CONFIG")
	if err != nil {
		log.Fatalf("Failed to create HTML modifier: %v", err)
	}

	// Configure the SPA server with a target for modification
	config := spaserve.SpaServerConfig{
		FS: distFS,
		Targets: []spaserve.TargetConfig{
			{
				TargetFile:  "index.html", // Specify the file to modify (relative to FS root)
				Modifier:    htmlModifier, // The modifier to apply
				CacheResult: true,         // Cache the modified index.html
			},
		},
		// SpaFallbackPath defaults to "index.html"
		// BasePath defaults to "/"
	}

	spaHandler, err := spaserve.NewSpaServer(config)
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

Inject multiple variables or apply different modifications to the same file.

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

	// Runtime data
	appEnv := AppConfig{ /* ... populate ... */ }
	userSession := map[string]string{"userId": "user-123", "role": "admin"}

	// Create individual modifiers
	modifier1, err1 := spaserve.NewHtmlScriptTagEnvModifier(appEnv, "APP_CONFIG")
	modifier2, err2 := spaserve.NewHtmlScriptTagEnvModifier(userSession, "USER_SESSION")
	if err1 != nil || err2 != nil {
		log.Fatalf("Failed to create modifiers: %v, %v", err1, err2)
	}

	// Combine them using a CompositeModifier
	compositeModifier := spaserve.CreateCompositeModifier(modifier1, modifier2)

	config := spaserve.SpaServerConfig{
		FS: distFS,
		Targets: []spaserve.TargetConfig{
			{
				TargetFile:  "index.html",
				Modifier:    compositeModifier, // Use the composite modifier
				CacheResult: true,
			},
			// You could add other targets here for different files
			// {
			//   TargetFile: "assets/config.js",
			//   Modifier: myCustomJsModifier,
			//   CacheResult: true,
			// },
		},
	}

	spaHandler, err := spaserve.NewSpaServer(config)
	// ... handle error, start server ...
}

```

### Example 4: Serving from OS Filesystem with Base Path

```go
package main

import (
	"log"
	"net/http"
	"os" // Use os.DirFS

	"github.com/jrschumacher/go-spaserve"
)


func main() {
	// Serve files from the local "./static-assets" directory
	osFS := os.DirFS("./static-assets")

	// Configure the SPA server
	config := spaserve.SpaServerConfig{
		FS: osFS,
		// Serve everything under the /myapp/ URL prefix
		// Must start and end with '/' unless it's just "/"
		BasePath: "/myapp/",
		// SpaFallbackPath defaults to "index.html"
	}

	spaHandler, err := spaserve.NewSpaServer(config)
	if err != nil {
		log.Fatal("Failed to create SPA handler:", err)
	}

	mux := http.NewServeMux()
	// IMPORTANT: The pattern here must match the BasePath!
	mux.Handle(config.BasePath, spaHandler)

	log.Println("Starting OS FS server on :8080 under /myapp/ ...")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
```

## Configuration (`SpaServerConfig`)

The `NewSpaServer` function accepts a `SpaServerConfig` struct with the following fields:

*   **`FS fs.FS`** (Required): The filesystem containing your static assets. Use `embed.FS`, `os.DirFS`, or any other `fs.FS` implementation.
*   **`Targets []TargetConfig`** (Optional): A slice defining which files to modify. Each `TargetConfig` has:
    *   `TargetFile string`: Path within the `FS` to modify (e.g., `"index.html"`, `"assets/config.js"`). Paths are cleaned internally.
    *   `Modifier FileModifier`: An implementation of the `FileModifier` interface responsible for transforming the file content. Use `NewHtmlScriptTagEnvModifier` or implement your own.
    *   `CacheResult bool`: If `true`, the result of the `Modifier` is cached in memory after the first request. If `false`, the `Modifier` runs on every request for that file.
*   **`SpaFallbackPath string`** (Optional): The path (relative to the `FS` root) to serve when a requested path looks like an SPA route (doesn't exist as a file and has no file extension).
    *   Defaults to `"index.html"`.
*   **`BasePath string`** (Optional): A URL prefix under which the application is served. Requests must start with this path, and the prefix is stripped before looking up files in the `FS`.
    *   Defaults to `"/"`.
    *   **Important:** If set to anything other than `/`, it *must* start and end with a `/` (e.g., `/myapp/`, `/admin/ui/`).
*   **`Logger *slog.Logger`** (Optional): A structured logger instance.
    *   Defaults to `slog.Default()`.
*   **`MuxErrorHandler func(statusCode int, w http.ResponseWriter, r *http.Request)`** (Optional): A function to handle HTTP errors generated by the `spaserve` handlers (e.g., 500 on modification failure, 404 if a targeted file isn't found in the source FS).
    *   Defaults to a simple `http.Error` response.

## Extensibility (`FileModifier` Interface)

You can create custom file modifications by implementing the `FileModifier` interface:

```go
package spaserve

type FileModifier interface {
	// Modify takes the original file path and content,
	// returns the modified content or an error.
	Modify(path string, originalContent []byte) (modifiedContent []byte, err error)
}
```

**Example: Placeholder Replacer**

```go
package main

import (
	"bytes"
	"fmt"

	"github.com/jrschumacher/go-spaserve" // Assuming used within the same project structure
)

type PlaceholderModifier struct {
	Placeholder string
	Value       string
}

func (pm *PlaceholderModifier) Modify(path string, originalContent []byte) ([]byte, error) {
	// Simple, potentially inefficient replacement for demonstration
	modified := bytes.ReplaceAll(originalContent, []byte(pm.Placeholder), []byte(pm.Value))
	if bytes.Equal(modified, originalContent) {
		fmt.Printf("Warning: Placeholder %q not found in file %q\n", pm.Placeholder, path)
	}
	return modified, nil
}

// Usage in SpaServerConfig:
// ...
// versionModifier := &PlaceholderModifier{ Placeholder: "__APP_VERSION__", Value: "1.2.3" }
// config.Targets = []spaserve.TargetConfig {
//   { TargetFile: "main.js", Modifier: versionModifier, CacheResult: true },
// }
// ...
```

This allows for various transformations like replacing placeholders, modifying CSS variables, processing template files, etc., all performed server-side at runtime.

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

This project is licensed under the MIT License - see the LICENSE file for details.
