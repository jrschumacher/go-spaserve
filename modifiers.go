package spaserve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// --- Composite Modifier ---

// CompositeModifier applies a list of modifiers in sequence.
type CompositeModifier struct {
	Modifiers []FileModifier
// 	logger    internalLogger // Add logger for better visibility
}

// NewCompositeModifier creates a modifier that chains others.
// It's recommended to provide a logger via config or it will default.
func NewCompositeModifier(modifiers ...FileModifier) FileModifier {
	// Note: Logger isn't easily available here unless passed explicitly or via context.
	// For simplicity, we'll rely on modifiers passed in potentially having their own logging.
	// A more advanced setup might inject the logger from the main config.
	return &CompositeModifier{Modifiers: modifiers}
}

func (cm *CompositeModifier) Modify(path string, originalContent []byte) ([]byte, error) {
	currentContent := originalContent
	var err error
	for i, modifier := range cm.Modifiers {
		currentContent, err = modifier.Modify(path, currentContent)
		if err != nil {
			// Wrap the error for context
			return nil, fmt.Errorf("composite modifier step %d failed for path %s: %w", i, path, err)
		}
	}
	return currentContent, nil
}

// Ensure CompositeModifier implements the interface (compile-time check)
var _ FileModifier = (*CompositeModifier)(nil)

// --- HTML Script Modifier ---

// HtmlScriptTagEnvModifier injects a JavaScript variable assignment into the <head> of an HTML document.
type HtmlScriptTagEnvModifier struct {
	Env       any    // The data structure to marshal into JSON.
	Namespace string // The JavaScript global variable name (e.g., "APP_CONFIG").
}

// NewHtmlScriptTagEnvModifier creates a FileModifier that injects env data into an HTML file.
// 'env' will be JSON marshaled. 'namespace' must be a valid JS identifier.
func NewHtmlScriptTagEnvModifier(env any, namespace string) (*HtmlScriptTagEnvModifier, error) {
	if namespace == "" {
		namespace = defaultStaticFilesHandlerOpts.ns
	}
	if !identifierRegex.MatchString(namespace) {
		return nil, fmt.Errorf("%w: '%s'", ErrInvalidNamespace, namespace)
	}
	return &HtmlScriptTagEnvModifier{
		Env:       env,
		Namespace: namespace,
	}, nil
}

func (hsm *HtmlScriptTagEnvModifier) Modify(path string, originalContent []byte) ([]byte, error) {
	// 1. Marshal the environment data
	envJsonBytes, err := json.Marshal(hsm.Env)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCouldNotMarshalEnv, err)
	}
	scriptContent := fmt.Sprintf("window.%s = %s;", hsm.Namespace, string(envJsonBytes))

	// 2. Construct the script tag node
	scriptNode := &html.Node{
		Type: html.ElementNode,
		Data: "script",
		Attr: []html.Attribute{{Key: "type", Val: "text/javascript"}},
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: scriptContent,
		},
	}

	// 3. Parse the original HTML
	doc, err := html.Parse(bytes.NewReader(originalContent))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCouldNotParseHtml, err)
	}

	// 4. Find the head tag
	headNode := findHtmlNode(doc, "head")
	if headNode == nil {
		// If no <head>, maybe try finding <html> or even <body> as a last resort?
		// Or just fail as finding <head> is the most reliable place.
		htmlNode := findHtmlNode(doc, "html")
		if htmlNode != nil {
			// Prepend to <html> if <head> missing
			headNode = htmlNode
			headNode.InsertBefore(scriptNode, headNode.FirstChild)
		} else {
			return nil, ErrCouldNotFindHead
		}
	} else {
		// 5. Prepend the script tag to the head
		headNode.InsertBefore(scriptNode, headNode.FirstChild)
	}

	// 6. Render the modified HTML back to bytes
	var buffer bytes.Buffer
	if err := html.Render(&buffer, doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCouldNotRenderHtml, err)
	}

	return buffer.Bytes(), nil
}

// findHtmlNode recursively searches for the first node with the given tag name.
func findHtmlNode(n *html.Node, tagName string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.ToLower(n.Data) == tagName {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findHtmlNode(c, tagName); found != nil {
			return found
		}
	}
	return nil
}

// Ensure HtmlScriptTagEnvModifier implements the interface (compile-time check)
var _ FileModifier = (*HtmlScriptTagEnvModifier)(nil)
