package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"github.com/jrschumacher/go-spaserve"
)

//go:embed ui/*
var embeddedFiles embed.FS

// Define constants for application configuration
const (
	// Frontend application environment variables
	API_ENDPOINT     = "https://api.example.com/v1"
	ENABLE_FEATURE_X = true
	APP_CONFIG_NS    = "APP_CONFIG" // JavaScript global variable name for app config

	// CSP Nonce configuration
	CSP_NONCE_LENGTH      = 16              // Number of random bytes for the nonce (hex string will be twice this length)
	CSP_NONCE_PLACEHOLDER = "__CSP_NONCE__" // String to replace with the generated nonce in content
)

// AppConfig defines the structure for frontend configuration injected into HTML.
type AppConfig struct {
	ApiEndpoint string `json:"apiEndpoint"`
	FeatureFlag bool   `json:"featureFlag"`
	ServerTime  string `json:"serverTime"` // Example of dynamic data
}

func main() {
	// 1. Get the embedded filesystem for static assets
	distFS, err := fs.Sub(embeddedFiles, "ui/dist")
	if err != nil {
		log.Fatalf("Failed to get sub filesystem: %v", err)
	}

	// 2. Populate application environment data for injection
	appEnv := AppConfig{
		ApiEndpoint: API_ENDPOINT,
		FeatureFlag: ENABLE_FEATURE_X,
		ServerTime:  fmt.Sprintf("Server started at %s", http.TimeFormat), // Example dynamic value
	}

	// 3. Create the HTML script modifier
	// This modifier will inject `window.APP_CONFIG = {...};` into `index.html`.
	htmlEnvModifier, err := spaserve.CreateHtmlScriptTagEnvModifier(appEnv, APP_CONFIG_NS)
	if err != nil {
		log.Fatalf("Failed to create HTML environment modifier: %v", err)
	}

	// 4. Create the CSP content nonce modifier
	// This modifier is responsible for:
	// - Generating a unique nonce for each request (if not cached).
	// - Adding `nonce` attributes to appropriate HTML tags (<script>, <style>, <link rel="stylesheet">).
	// - Converting inline `style` attributes into new nonce-protected `<style>` tags.
	// - Storing the generated nonce in the `FileModifierContext.Scratch` map for other modifiers to use.
	// - Replacing occurrences of `CSP_NONCE_PLACEHOLDER` in the HTML content with the actual nonce.
	// - Automatically adding `'nonce-...'` sources to the `script-src` and `style-src` directives
	//   in the `Content-Security-Policy` header.
	nonceContentModifier := spaserve.NewCSPContentNonceModifier(spaserve.CSPContentNonceModifierOptions{
		NonceLength:             CSP_NONCE_LENGTH,
		NonceStringReplacements: []string{CSP_NONCE_PLACEHOLDER},
	})

	// 5. Create a custom CSP header modifier for *other* directives.
	// This modifier defines additional CSP directives beyond what `nonceContentModifier` handles.
	// It should *not* manually add nonce-related sources (e.g., `script-src 'nonce-...'`),
	// as `nonceContentModifier` will do that and merge them correctly.
	customCSPModifier := spaserve.NewCSPResponseHeaderModifier(func(context spaserve.FileModifierContext) (string, error) {
		// Define your base CSP directives here.
		// Example: Allow images from self and data URIs, allow websocket connections
		return "default-src 'self'; img-src 'self' data:; connect-src 'self' ws:;", nil
	})

	// 6. Combine all modifiers using a CompositeModifier.
	// The order of modifiers in the CompositeModifier matters for content transformation.
	// - Content modifications (like `htmlEnvModifier`, `nonceContentModifier`) should run first,
	//   in the order they should apply.
	// - `nonceContentModifier` also acts as a header modifier, which will run *after* all content
	//   modifiers, along with `customCSPModifier`. The `CompositeModifier` handles merging
	//   all header modifications into a single `Content-Security-Policy` header.
	compositeModifier := spaserve.CreateCompositeModifier(
		htmlEnvModifier,      // Content Modifier 1: Inject app config
		nonceContentModifier, // Content Modifier 2: Adds nonces to HTML, converts inline styles, stores nonce in scratch.
		customCSPModifier,    // Header Modifier: Adds other user-defined CSP directives. (Nonce directives are added by nonceContentModifier).
	)

	// 7. Define the target configuration for `index.html`.
	// This specifies that our `compositeModifier` should be applied to `index.html`.
	targets := []spaserve.TargetConfig{
		{
			TargetFile:  "index.html", // Apply all these modifiers to index.html
			Modifier:    compositeModifier,
			CacheResult: false, // We do not want to cache CSP updates
		},
	}

	// 8. Configure and create the SPA server handler.
	spaHandler, err := spaserve.NewSpaServer(
		distFS,                                     // The embedded filesystem
		spaserve.WithTargets(targets),              // Apply our composite modifier
		spaserve.WithSpaFallbackPath("index.html"), // Fallback for client-side routes
		// spaserve.WithBasePath("/app/"), // Uncomment if you want to serve under a base path
		// spaserve.WithLogger(slog.Default()), // Uncomment for custom logging
	)
	if err != nil {
		log.Fatalf("Failed to create SPA handler: %v", err)
	}

	// 9. Set up the HTTP server
	mux := http.NewServeMux()
	mux.Handle("/", spaHandler) // Handle all requests with our SPA handler

	log.Println("Starting server with CSP nonce and env injection on :8080...")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
