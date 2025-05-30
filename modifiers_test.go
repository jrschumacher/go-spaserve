package spaserve

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// --- HtmlScriptModifier Tests ---

func TestHtmlScriptModifier_Modify_Success(t *testing.T) {
	env := map[string]string{"api": "http://localhost:8080", "feature": "true"}
	ns := "APP_CONFIG"
	modifier, err := NewHtmlScriptTagEnvModifier(env, ns)
	if err != nil {
		t.Fatalf("NewHtmlScriptTagEnvModifier failed: %v", err)
	}

	htmlInput := `
<!DOCTYPE html>
<html>
<head>
    <title>Test</title>
    <link rel="stylesheet" href="style.css">
</head>
<body>
    <h1>Hello</h1>
</body>
</html>`

	expectedScriptContent := `window.APP_CONFIG = {"api":"http://localhost:8080","feature":"true"};`

	modifiedBytes, err := modifier.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, []byte(htmlInput))
	if err != nil {
		t.Fatalf("Modify failed: %v", err)
	}

	modifiedHtml := string(modifiedBytes)

	if !strings.Contains(modifiedHtml, expectedScriptContent) {
		t.Errorf("Modified HTML does not contain the expected script content.\nExpected to contain: %s\nGot:\n%s", expectedScriptContent, modifiedHtml)
	}

	// Check if script is inside <head> (approximate check)
	headStartIndex := strings.Index(modifiedHtml, "<head>")
	headEndIndex := strings.Index(modifiedHtml, "</head>")
	scriptIndex := strings.Index(modifiedHtml, "<script ")
	if !(headStartIndex != -1 && headEndIndex != -1 && scriptIndex > headStartIndex && scriptIndex < headEndIndex) {
		t.Errorf("Script tag not found within the head tag.\nGot:\n%s", modifiedHtml)
	}
}

func TestHtmlScriptModifier_Modify_NoHeadAddsToHtml(t *testing.T) {
	modifier, err := NewHtmlScriptTagEnvModifier(map[string]bool{"ok": true}, "CONFIG")
	if err != nil {
		t.Fatalf("NewHtmlScriptTagEnvModifier failed: %v", err)
	}
	htmlInput := `<html><body><p>Test</p></body></html>`
	expectedScriptContent := `window.CONFIG = {"ok":true};`

	modifiedBytes, err := modifier.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, []byte(htmlInput))
	if err != nil {
		t.Fatalf("Modify failed unexpectedly: %v", err)
	}
	modifiedHtml := string(modifiedBytes)
	if !strings.Contains(modifiedHtml, expectedScriptContent) {
		t.Errorf("Modified HTML does not contain the expected script content.\nExpected: %s\nGot:\n%s", expectedScriptContent, modifiedHtml)
	}
	// Check it's inside <html> but before <body>
	htmlStartIndex := strings.Index(modifiedHtml, "<html") // allow attributes
	bodyStartIndex := strings.Index(modifiedHtml, "<body")
	scriptIndex := strings.Index(modifiedHtml, "<script ")
	if !(htmlStartIndex != -1 && bodyStartIndex != -1 && scriptIndex > htmlStartIndex && scriptIndex < bodyStartIndex) {
		t.Errorf("Script tag not found within html tag before body tag.\nGot:\n%s", modifiedHtml)
	}
}

func TestHtmlScriptModifier_Modify_InvalidNamespace(t *testing.T) {
	_, err := NewHtmlScriptTagEnvModifier(map[string]string{}, "invalid-ns")
	if err == nil {
		t.Errorf("Expected error for invalid namespace, but got nil")
	} else if !errors.Is(err, ErrInvalidNamespace) {
		t.Errorf("Expected error type %v, got %v", ErrInvalidNamespace, err)
	}
}

func TestHtmlScriptModifier_Modify_JsonMarshalError(t *testing.T) {
	modifier, _ := NewHtmlScriptTagEnvModifier(make(chan int), "CONFIG") // Channels can't be marshalled
	_, err := modifier.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, []byte("<html><head></head><body></body></html>"))
	if err == nil {
		t.Errorf("Expected error for JSON marshalling failure, but got nil")
	} else if !errors.Is(err, ErrCouldNotMarshalEnv) {
		t.Errorf("Expected error type %v, got %v", ErrCouldNotMarshalEnv, err)
	}
}

// Golang's HTML parser attempts to be spec-compliant and will create nodes that do not exist.
// func TestHtmlScriptModifier_Modify_HtmlParseError(t *testing.T) {
// 	modifier, _ := NewHtmlScriptTagEnvModifier(map[string]string{}, "CONFIG")
// 	_, err := modifier.ModifyContent()("index.html", []byte("<html><head><body></html>")) // Malformed HTML
// 	if err == nil {
// 		t.Errorf("Expected error for HTML parsing failure, but got nil")
// 	} else if !errors.Is(err, ErrCouldNotParseHtml) {
// 		t.Errorf("Expected error type %v, got %v", ErrCouldNotParseHtml, err)
// 	}
// }
//
// func TestHtmlScriptModifier_Modify_NoHeadOrHtmlError(t *testing.T) {
// 	modifier, _ := NewHtmlScriptTagEnvModifier(map[string]string{}, "CONFIG")
// 	res, err := modifier.ModifyContent()("index.html", []byte("Just text"))
// 	if err == nil {
// 		t.Errorf("Expected error for missing head/html tag, but got nil from: %q", res)
// 	} else if !errors.Is(err, ErrCouldNotFindHead) {
// 		t.Errorf("Expected error type %v, got %v", ErrCouldNotFindHead, err)
// 	}
// }

// --- Composite Modifier Tests ---

// Mock modifier for composite tests
type mockStringModifier struct {
	append string
	fail   bool
}

func (m *mockStringModifier) ModifyContent(context FileModifierContext, originalContent []byte) ([]byte, error) {
	if m.fail {
		return nil, fmt.Errorf("mock fail for %s", context.Request.Path)
	}
	return append(originalContent, []byte(m.append)...), nil
}

func TestCompositeModifier_Modify_Success(t *testing.T) {
	mod1 := &mockStringModifier{append: "::Mod1"}
	mod2 := &mockStringModifier{append: "::Mod2"}
	composite := NewCompositeModifier(mod1, mod2)

	input := []byte("Start")
	expected := []byte("Start::Mod1::Mod2")

	result, err := composite.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, input)
	if err != nil {
		t.Fatalf("Composite Modify failed: %v", err)
	}
	if string(result) != string(expected) {
		t.Errorf("Expected %q, got %q", string(expected), string(result))
	}
}

func TestCompositeModifier_Modify_Empty(t *testing.T) {
	composite := NewCompositeModifier() // No modifiers
	input := []byte("Start")

	result, err := composite.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, input)
	if err != nil {
		t.Fatalf("Empty Composite Modify failed: %v", err)
	}
	if string(result) != string(input) {
		t.Errorf("Expected %q, got %q", string(input), string(result))
	}
}

func TestCompositeModifier_Modify_ErrorPropagation(t *testing.T) {
	mod1 := &mockStringModifier{append: "::Mod1"}
	modFail := &mockStringModifier{fail: true}
	mod3 := &mockStringModifier{append: "::Mod3"} // Should not run
	composite := NewCompositeModifier(mod1, modFail, mod3)

	input := []byte("Start")
	_, err := composite.ModifyContent(FileModifierContext{
		Request: FileModifierContextRequest{
			Headers: make(http.Header),
			Path:    "test.txt",
		},
		Scratch: make(map[string]any),
	}, input)

	if err == nil {
		t.Fatalf("Expected composite modifier to fail, but got nil error")
	}
	// Check if the error message indicates which step failed (or wraps the original error)
	if !strings.Contains(err.Error(), "mock fail") {
		t.Errorf("Expected error message to contain 'mock fail', got: %v", err)
	}
	if !strings.Contains(err.Error(), "step 1 (ModifyContent) failed") { // Modifiers are 0-indexed
		t.Errorf("Expected error message to indicate step 1 failed, got: %v", err)
	}
}

// --- Helper Functions for HTML and CSP Parsing ---

// getAttributeValue retrieves the value of a specific attribute from the first matching node.
func getAttributeValue(htmlContent string, tagName, attributeKey string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", false
	}

	var attrVal string
	var found bool
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tagName {
			for _, attr := range n.Attr {
				if attr.Key == attributeKey {
					attrVal = attr.Val
					found = true
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found { // Optimization: stop recursion if found
				return
			}
			f(c)
		}
	}
	f(doc)
	return attrVal, found
}

// getAllNoncesFromHtmlTag finds all 'nonce' attributes on elements of a given tag name.
func getAllNoncesFromHtmlTag(htmlContent string, tagName string) []string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var nonces []string
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tagName {
			for _, attr := range n.Attr {
				if attr.Key == "nonce" {
					nonces = append(nonces, attr.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
	return nonces
}

// getStyleTagContent gets the text content of the first <style> tag.
func getStyleTagContent(htmlContent string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return "", false
	}
	var content string
	var found bool
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			content = n.FirstChild.Data
			found = true
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found {
				return
			}
			f(c)
		}
	}
	f(doc)
	return content, found
}

// extractCSPNonce extracts the nonce value from a Content-Security-Policy header.
func extractCSPNonce(cspHeader string) (string, bool) {
	if cspHeader == "" {
		return "", false
	}
	directives := strings.Split(cspHeader, ";")
	for _, dir := range directives {
		dir = strings.TrimSpace(dir)
		// Look for `script-src` or `style-src` directives that contain a nonce
		if strings.HasPrefix(dir, "script-src") || strings.HasPrefix(dir, "style-src") {
			parts := strings.Fields(dir) // Splits by whitespace
			for _, part := range parts {
				if strings.HasPrefix(part, "'nonce-") && strings.HasSuffix(part, "'") {
					return strings.TrimPrefix(strings.TrimSuffix(part, "'"), "'nonce-"), true
				}
			}
		}
	}
	return "", false
}

// --- Mock Implementations ---

// mockNonceDecider for testing NonceElementDecider interface.
type mockNonceDecider struct {
	shouldModify bool
	calledCount  int
}

func (m *mockNonceDecider) ShouldModify(node *html.Node) bool {
	m.calledCount++
	return m.shouldModify
}

// selectiveNonceDecider allows nonces for elements with specific IDs or for stylesheets.
type selectiveNonceDecider struct{}

func (s *selectiveNonceDecider) ShouldModify(node *html.Node) bool {
	for _, attr := range node.Attr {
		if attr.Key == "id" {
			if attr.Val == "always-add" || attr.Val == "always-add-style" || attr.Val == "always-add-inline" {
				return true
			}
		}
	}
	// For link tags, specifically check if it's a stylesheet
	if node.Data == "link" {
		for _, attr := range node.Attr {
			if attr.Key == "rel" && attr.Val == "stylesheet" {
				return true // Always allow for stylesheets in this test
			}
		}
	}
	return false // Deny by default
}

// --- Test Cases for CSPContentNonceModifier ---

func TestCSPContentNonceModifier_NewCSPContentNonceModifier(t *testing.T) {
	tests := []struct {
		name                          string
		options                       CSPContentNonceModifierOptions
		expectedNonceLength           int
		expectNonceElementDecider     bool
		expectNonceStringReplacements []string
	}{
		{
			name:                          "Basic initialization",
			options:                       CSPContentNonceModifierOptions{},
			expectedNonceLength:           0, // Should be 0, default applied in ModifyContent
			expectNonceElementDecider:     false,
			expectNonceStringReplacements: nil,
		},
		{
			name: "With custom NonceLength",
			options: CSPContentNonceModifierOptions{
				NonceLength: 16,
			},
			expectedNonceLength:           16,
			expectNonceElementDecider:     false,
			expectNonceStringReplacements: nil,
		},
		{
			name: "With NonceElementDecider",
			options: CSPContentNonceModifierOptions{
				NonceElementDecider: &mockNonceDecider{shouldModify: true},
			},
			expectedNonceLength:           0,
			expectNonceElementDecider:     true,
			expectNonceStringReplacements: nil,
		},
		{
			name: "With NonceStringReplacements",
			options: CSPContentNonceModifierOptions{
				NonceStringReplacements: []string{"__NONCE_HERE__"},
			},
			expectedNonceLength:           0,
			expectNonceElementDecider:     false,
			expectNonceStringReplacements: []string{"__NONCE_HERE__"},
		},
		{
			name: "All options set",
			options: CSPContentNonceModifierOptions{
				NonceLength: 8,
				NonceElementDecider: &mockNonceDecider{
					shouldModify: false,
				},
				NonceStringReplacements: []string{"A", "B"},
			},
			expectedNonceLength:           8,
			expectNonceElementDecider:     true,
			expectNonceStringReplacements: []string{"A", "B"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := NewCSPContentNonceModifier(tt.options)

			if modifier == nil {
				t.Fatal("NewCSPContentNonceModifier returned nil")
			}
			if modifier.nonceLength != tt.expectedNonceLength {
				t.Errorf("Expected nonceLength %d, got %d", tt.expectedNonceLength, modifier.nonceLength)
			}
			if (modifier.nonceElementDecider != nil) != tt.expectNonceElementDecider {
				t.Errorf("Expected nonceElementDecider presence %t, got %t", tt.expectNonceElementDecider, modifier.nonceElementDecider != nil)
			}
			if !compareStringSlices(modifier.nonceStringReplacements, tt.expectNonceStringReplacements) {
				t.Errorf("Expected NonceStringReplacements %v, got %v", tt.expectNonceStringReplacements, modifier.nonceStringReplacements)
			}
		})
	}
}

func TestCSPContentNonceModifier_ModifyContent(t *testing.T) {
	baseHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>Test</title>
    <script>console.log('inline script 1');</script>
    <link rel="stylesheet" href="style.css">
    <link rel="icon" href="favicon.ico">
</head>
<body>
    <p style="color:red; font-size: 16px;">Inline style text</p>
    <div class="container" style="margin: 10px;">Another inline style</div>
    <script src="app.js"></script>
    <style>body { margin: 0; }</style>
    <span class="foo">Some text with __NONCE_PLACEHOLDER__ inside.</span>
</body>
</html>`

	tests := []struct {
		name                          string
		options                       CSPContentNonceModifierOptions
		inputHTML                     string
		expectedScriptNonces          int // Expected count of nonce attributes on <script>
		expectedStyleNonces           int // Expected count of nonce attributes on <style> (including generated ones)
		expectedLinkNonces            int // Expected count of nonce attributes on <link> rel="stylesheet"
		expectNonceInScratch          bool
		expectStringReplaced          bool
		expectInlineStyleHandled      bool
		expectOriginalInlineStyleGone bool
	}{
		{
			name:                          "Adds nonce to script, style, link rel=stylesheet and handles inline styles",
			options:                       CSPContentNonceModifierOptions{},
			inputHTML:                     baseHTML,
			expectedScriptNonces:          2, // <script>, <script src>
			expectedStyleNonces:           3, // <style>, new <style> for <p>, new <style> for <div>
			expectedLinkNonces:            1, // <link rel="stylesheet">
			expectNonceInScratch:          true,
			expectInlineStyleHandled:      true,
			expectOriginalInlineStyleGone: true,
		},
		{
			name:                          "Custom NonceLength",
			options:                       CSPContentNonceModifierOptions{NonceLength: 8},
			inputHTML:                     baseHTML,
			expectedScriptNonces:          2, // <script>, <script src>
			expectedStyleNonces:           3, // <style>, new <style> for <p>, new <style> for <div>
			expectedLinkNonces:            1, // <link rel="stylesheet">
			expectNonceInScratch:          true,
			expectInlineStyleHandled:      true,
			expectOriginalInlineStyleGone: true,
		},
		{
			name:                          "Custom NonceLength",
			options:                       CSPContentNonceModifierOptions{NonceStringReplacements: []string{"__NONCE_PLACEHOLDER__"}},
			inputHTML:                     baseHTML,
			expectedScriptNonces:          2, // <script>, <script src>
			expectedStyleNonces:           3, // <style>, new <style> for <p>, new <style> for <div>
			expectedLinkNonces:            1, // <link rel="stylesheet">
			expectNonceInScratch:          true,
			expectInlineStyleHandled:      true,
			expectOriginalInlineStyleGone: true,
			expectStringReplaced:          true,
		},
		{
			name:                          "No eligible tags",
			options:                       CSPContentNonceModifierOptions{},
			inputHTML:                     `<html><body><p>Hello</p></body></html>`,
			expectedScriptNonces:          0,
			expectedStyleNonces:           0,
			expectedLinkNonces:            0,
			expectNonceInScratch:          true, // Nonce should still be generated and stored
			expectInlineStyleHandled:      false,
			expectOriginalInlineStyleGone: false,
		},
		{
			name:                          "Html parse error - malformed HTML (parser resilient)",
			options:                       CSPContentNonceModifierOptions{},
			inputHTML:                     `<html><head><script>alert(1);</script><body>`, // Missing </head>, </html>
			expectedScriptNonces:          1,                                              // Parser is resilient, will fix and still apply
			expectedStyleNonces:           0,
			expectedLinkNonces:            0,
			expectNonceInScratch:          true,
			expectInlineStyleHandled:      false,
			expectOriginalInlineStyleGone: false,
		},
		{
			name: "Nonce string replacement",
			options: CSPContentNonceModifierOptions{
				NonceStringReplacements: []string{"__NONCE_PLACEHOLDER__"},
			},
			inputHTML:                     `<html><body><p>The nonce is __NONCE_PLACEHOLDER__ here.</p></body></html>`,
			expectedScriptNonces:          0,
			expectedStyleNonces:           0,
			expectedLinkNonces:            0,
			expectNonceInScratch:          true,
			expectStringReplaced:          true,
			expectInlineStyleHandled:      false,
			expectOriginalInlineStyleGone: false,
		},
		{
			name: "No string replacements configured",
			options: CSPContentNonceModifierOptions{
				NonceStringReplacements: nil,
			},
			inputHTML:                     `<html><body><p>The nonce is __NONCE_PLACEHOLDER__ here.</p></body></html>`,
			expectedScriptNonces:          0,
			expectedStyleNonces:           0,
			expectedLinkNonces:            0,
			expectNonceInScratch:          true,
			expectStringReplaced:          false, // Should not replace
			expectInlineStyleHandled:      false,
			expectOriginalInlineStyleGone: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := NewCSPContentNonceModifier(tt.options)
			ctx := FileModifierContext{
				Request: FileModifierContextRequest{Path: "index.html"},
				Scratch: make(map[string]any),
			}

			modifiedContent, err := modifier.ModifyContent(ctx, []byte(tt.inputHTML))
			if err != nil {
				t.Fatalf("ModifyContent failed: %v", err)
			}

			// Verify nonce is stored in scratch
			var generatedNonce string
			if tt.expectNonceInScratch {
				nonceVal, ok := ctx.Scratch["nonce"]
				if !ok {
					t.Fatal("Expected nonce to be stored in scratch context")
				}
				var isString bool
				generatedNonce, isString = nonceVal.(string)
				if !isString || generatedNonce == "" {
					t.Fatalf("Expected nonce in scratch to be a non-empty string, got %v", nonceVal)
				}
				// Verify nonce length if specified
				if tt.options.NonceLength > 0 && len(generatedNonce) != tt.options.NonceLength*2 { // hex encoded, so *2
					t.Errorf("Expected nonce length %d (hex %d), got %d", tt.options.NonceLength, tt.options.NonceLength*2, len(generatedNonce))
				}
			}

			// Verify nonce attributes on tags
			scriptNonces := getAllNoncesFromHtmlTag(string(modifiedContent), "script")
			if len(scriptNonces) != tt.expectedScriptNonces {
				t.Errorf("Expected %d script nonce attributes, got %d. Nonces: %v", tt.expectedScriptNonces, len(scriptNonces), scriptNonces)
			}
			for _, n := range scriptNonces {
				if n != generatedNonce {
					t.Errorf("Script nonce mismatch: expected %q, got %q", generatedNonce, n)
				}
			}

			styleNonces := getAllNoncesFromHtmlTag(string(modifiedContent), "style")
			if len(styleNonces) != tt.expectedStyleNonces {
				t.Errorf("Expected %d style nonce attributes, got %d. Nonces: %v", tt.expectedStyleNonces, len(styleNonces), styleNonces)
			}
			for _, n := range styleNonces {
				if n != generatedNonce {
					t.Errorf("Style nonce mismatch: expected %q, got %q", generatedNonce, n)
				}
			}

			linkNonces := getAllNoncesFromHtmlTag(string(modifiedContent), "link")
			if len(linkNonces) != tt.expectedLinkNonces {
				t.Errorf("Expected %d link nonce attributes, got %d. Nonces: %v", tt.expectedLinkNonces, len(linkNonces), linkNonces)
			}
			for _, n := range linkNonces {
				if n != generatedNonce {
					t.Errorf("Link nonce mismatch: expected %q, got %q", generatedNonce, n)
				}
			}

			// Check that link rel="icon" did *not* get a nonce
			if strings.Contains(string(modifiedContent), `rel="icon" nonce=`) {
				t.Error("Link rel=icon should not have received a nonce")
			}

			// Verify string replacement
			if tt.expectStringReplaced {
				if strings.Contains(string(modifiedContent), "__NONCE_PLACEHOLDER__") {
					t.Error("String replacement placeholder found, but expected it to be replaced.")
				}
				if !strings.Contains(string(modifiedContent), generatedNonce) {
					t.Error("Generated nonce not found in content after replacement.")
				}
			}

			// Verify inline style handling
			if tt.expectInlineStyleHandled {
				// Original style attributes should be gone
				if styleAttr, found := getAttributeValue(string(modifiedContent), "p", "style"); found && styleAttr != "" {
					t.Errorf("Expected inline style attribute to be removed from <p> tag, but it's still there with value: %q.", styleAttr)
				}
				if styleAttr, found := getAttributeValue(string(modifiedContent), "div", "style"); found && styleAttr != "" {
					t.Errorf("Expected inline style attribute to be removed from <div> tag, but it's still there with value: %q.", styleAttr)
				}

				// New style tags with nonce should exist and contain the original style
				styleContentP, foundP := getStyleTagContent(string(modifiedContent))
				if !foundP || !strings.Contains(styleContentP, "color:red; font-size: 16px;") {
					t.Errorf("Expected new <style> tag for <p> inline style to contain original content.")
				}
				styleContentDiv, foundDiv := getStyleTagContent(strings.ReplaceAll(string(modifiedContent), styleContentP, "")) // Get the second style tag
				if !foundDiv || !strings.Contains(styleContentDiv, "margin: 10px;") {
					t.Errorf("Expected new <style> tag for <div> inline style to contain original content.")
				}

				// Original elements should have a class attribute (or appended to existing)
				pClass, _ := getAttributeValue(string(modifiedContent), "p", "class")
				if pClass == "" || !strings.HasPrefix(pClass, "_") {
					t.Errorf("Expected <p> tag to have a new class attribute starting with '_', got %q", pClass)
				}
				divClass, _ := getAttributeValue(string(modifiedContent), "div", "class")
				if divClass == "" || !strings.HasPrefix(divClass, "container _") { // Should append to existing class
					t.Errorf("Expected <div> tag to have existing class plus a new one starting with '_', got %q", divClass)
				}
			} else if tt.inputHTML == baseHTML {
				// If not handled, original inline styles should remain
				if styleAttr, found := getAttributeValue(string(modifiedContent), "p", "style"); !found || styleAttr == "" {
					t.Errorf("Expected inline style attribute to remain on <p> tag, but it's gone.")
				}
				if styleAttr, found := getAttributeValue(string(modifiedContent), "div", "style"); !found || styleAttr == "" {
					t.Errorf("Expected inline style attribute to remain on <div> tag, but it's gone.")
				}
				if strings.Contains(string(modifiedContent), "<style nonce=") {
					t.Errorf("Expected no new style tag for inline style when not handled")
				}
				if strings.Contains(string(modifiedContent), `class="_`) {
					t.Errorf("Expected no new class attribute when not handled")
				}
			}
		})
	}

	// Test for `makeNonce` error (difficult to mock `crypto/rand.Read` directly for a simple error path).
	// Current implementation of `makeNonce` based on `crypto/rand` is unlikely to return an error for valid lengths.
	// A length of 0 returns an empty string (valid), negative values would panic before error return.
}

func TestCSPContentNonceModifier_ModifyContent_NonceElementDecider(t *testing.T) {
	htmlContent := `
<html>
<head>
    <script id="always-add">console.log('script A');</script>
    <script id="never-add">console.log('script B');</script>
    <style id="always-add-style"></style>
    <link rel="stylesheet" href="style.css" id="always-add-link">
    <link rel="icon" href="favicon.ico">
</head>
<body>
    <div style="background: blue;" id="always-add-inline"></div>
    <div style="border: 1px solid red;" id="never-add-inline"></div>
</body>
</html>`

	t.Run("Decider always returns true", func(t *testing.T) {
		decider := &mockNonceDecider{shouldModify: true}
		modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{
			NonceElementDecider: decider,
		})
		ctx := FileModifierContext{Scratch: make(map[string]any)}
		modifiedContent, err := modifier.ModifyContent(ctx, []byte(htmlContent))
		if err != nil {
			t.Fatalf("ModifyContent failed: %v", err)
		}

		nonce := ctx.Scratch["nonce"].(string)

		if count := len(getAllNoncesFromHtmlTag(string(modifiedContent), "script")); count != 2 {
			t.Errorf("Expected 2 script nonces, got %d", count)
		}
		if count := len(getAllNoncesFromHtmlTag(string(modifiedContent), "style")); count != 3 { // 1 existing, 2 from inline
			t.Errorf("Expected 3 style nonces, got %d", count)
		}
		if count := len(getAllNoncesFromHtmlTag(string(modifiedContent), "link")); count != 1 { // Only stylesheet link
			t.Errorf("Expected 1 link nonce, got %d", count)
		}

		// Check specific elements have nonce
		if !strings.Contains(string(modifiedContent), `<script id="always-add" nonce="`+nonce+`"`) {
			t.Errorf("script#always-add missing nonce")
		}
		if !strings.Contains(string(modifiedContent), `<script id="never-add" nonce="`+nonce+`"`) {
			t.Errorf("script#never-add missing nonce") // Should have nonce because decider returned true
		}
		if !strings.Contains(string(modifiedContent), `<style id="always-add-style" nonce="`+nonce+`"`) {
			t.Errorf("style#always-add-style missing nonce")
		}
		if !strings.Contains(string(modifiedContent), `<link rel="stylesheet" href="style.css" id="always-add-link" nonce="`+nonce+`"`) {
			t.Errorf("link#always-add-link missing nonce")
		}
		if !strings.Contains(string(modifiedContent), `<style nonce="`+nonce+`">.`) {
			t.Errorf("New style tag for inline style missing nonce")
		}
		if attrValue, attrValueExists := getAttributeValue(string(modifiedContent), "p", "style"); attrValueExists && attrValue != "" {
			t.Errorf("Inline style attribute should be removed from p tag")
		}
		if attrValue, attrValueExists := getAttributeValue(string(modifiedContent), "div", "style"); attrValueExists && attrValue != "" {
			t.Errorf("Inline style attribute should be removed from div tag")
		}
	})

	t.Run("Decider always returns false", func(t *testing.T) {
		decider := &mockNonceDecider{shouldModify: false}
		modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{
			NonceElementDecider: decider,
		})
		ctx := FileModifierContext{Scratch: make(map[string]any)}
		modifiedContent, err := modifier.ModifyContent(ctx, []byte(htmlContent))
		if err != nil {
			t.Fatalf("ModifyContent failed: %v", err)
		}

		// Check no nonces added
		if len(getAllNoncesFromHtmlTag(string(modifiedContent), "script")) != 0 ||
			len(getAllNoncesFromHtmlTag(string(modifiedContent), "style")) != 0 ||
			len(getAllNoncesFromHtmlTag(string(modifiedContent), "link")) != 0 {
			t.Error("Expected no nonce attributes, but found some")
		}

		// Ensure inline styles were NOT converted if decider prevents it
		if styleAttr, found := getAttributeValue(string(modifiedContent), "div", "style"); !found || styleAttr == "" {
			t.Errorf("Expected inline style attribute to remain on <div> tag when decider returns false, but it's gone.")
		}
		if strings.Contains(string(modifiedContent), "<style nonce=") {
			t.Errorf("Expected no new style tag for inline style when decider returns false")
		}
		if strings.Contains(string(modifiedContent), `class="_`) {
			t.Errorf("Expected div with inline style to NOT get a class when decider returns false")
		}
	})

	t.Run("Decider selectively allows", func(t *testing.T) {
		decider := &selectiveNonceDecider{} // Selective logic defined above
		modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{
			NonceElementDecider: decider,
		})
		ctx := FileModifierContext{Scratch: make(map[string]any)}
		modifiedContent, err := modifier.ModifyContent(ctx, []byte(htmlContent))
		if err != nil {
			t.Fatalf("ModifyContent failed: %v", err)
		}

		nonce := ctx.Scratch["nonce"].(string)

		// Check script with id="always-add" should have nonce
		if !strings.Contains(string(modifiedContent), `<script id="always-add" nonce="`+nonce+`"`) {
			t.Errorf("Expected script A to have nonce, got:\n%s", string(modifiedContent))
		}
		// Check script with id="never-add" should NOT have nonce
		if strings.Contains(string(modifiedContent), `script id="never-add" nonce="`) {
			t.Errorf("Expected script B to NOT have nonce, got:\n%s", string(modifiedContent))
		}

		// Check style with id="always-add-style" should have nonce
		if !strings.Contains(string(modifiedContent), `<style id="always-add-style" nonce="`+nonce+`"`) {
			t.Errorf("Expected style A to have nonce, got:\n%s", string(modifiedContent))
		}

		// Check link rel="stylesheet" should have nonce (always true in selectiveDecider)
		if !strings.Contains(string(modifiedContent), `<link rel="stylesheet" href="style.css" id="always-add-link" nonce="`+nonce+`"`) {
			t.Errorf("Expected link rel=stylesheet to have nonce, got:\n%s", string(modifiedContent))
		}

		// Check inline style with id="always-add-inline" should be handled with nonce
		if !strings.Contains(string(modifiedContent), `<div id="always-add-inline" class=`) {
			t.Errorf("Expected inline-styled div A to be converted")
		}
		if !strings.Contains(string(modifiedContent), `<style nonce="`+nonce+`">.`) { // Check for new style tag with nonce
			t.Errorf("Expected new style tag for inline style A to have nonce")
		}

		// Check inline style with id="never-add-inline" should NOT be handled with nonce
		if !strings.Contains(string(modifiedContent), `<div style="border: 1px solid red;" id="never-add-inline">`) {
			t.Errorf("Expected inline-styled div B to NOT be converted")
		}
	})
}

func TestCSPContentNonceModifier_ModifyResponseHeaders(t *testing.T) {
	tests := []struct {
		name           string
		initialHeaders http.Header
		expectedCSP    string // Expected Content-Security-Policy header value pattern
		expectError    bool
	}{
		{
			name:           "No existing CSP",
			initialHeaders: http.Header{},
			expectedCSP:    "script-src 'nonce-%s'; style-src 'nonce-%s'",
		},
		{
			name: "Existing CSP - preserve directives",
			initialHeaders: http.Header{
				"Content-Security-Policy": {"default-src 'self'; img-src *;"},
			},
			expectedCSP: "default-src 'self'; img-src *; script-src 'nonce-%s'; style-src 'nonce-%s'",
		},
		{
			name: "Existing CSP - script-src already present",
			initialHeaders: http.Header{
				"Content-Security-Policy": {"script-src 'self' example.com; style-src 'self';"},
			},
			expectedCSP: "script-src 'self' example.com 'nonce-%s'; style-src 'self' 'nonce-%s'", // nonce should be appended
		},
		{
			name: "Existing CSP - multiple CSP headers (should merge)",
			initialHeaders: http.Header{
				"Content-Security-Policy": {"default-src 'self'", "img-src *; script-src 'self'"},
			},
			expectedCSP: "default-src 'self'; img-src *; script-src 'self' 'nonce-%s'; style-src 'nonce-%s'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{})
			// Simulate ModifyContent having run and populated the nonce
			testNonce := "randomtestnonce123"
			ctx := FileModifierContext{
				Request: FileModifierContextRequest{Headers: tt.initialHeaders.Clone()},
				Scratch: map[string]any{"nonce": testNonce},
			}

			modifiedHeaders, err := modifier.ModifyResponseHeaders(ctx)

			if tt.expectError {
				if err == nil {
					t.Error("Expected an error, but got nil")
				}
				return // Don't check headers if error expected
			} else if err != nil {
				t.Fatalf("ModifyResponseHeaders failed: %v", err)
			}

			cspHeaders := modifiedHeaders.Values("Content-Security-Policy")
			if len(cspHeaders) != 1 {
				t.Fatalf("Expected exactly one Content-Security-Policy header, got %d: %v", len(cspHeaders), cspHeaders)
			}

			csp := cspHeaders[0]
			expectedNonceSource := fmt.Sprintf("'nonce-%s'", testNonce)

			if !strings.Contains(csp, expectedNonceSource) {
				t.Errorf("CSP header does not contain expected nonce source %q.\nGot: %s", expectedNonceSource, csp)
			}

			// Verify script-src and style-src are present and contain the nonce.
			if !strings.Contains(csp, "script-src") {
				t.Errorf("CSP header missing script-src directive: %s", csp)
			}
			if !strings.Contains(csp, "style-src") {
				t.Errorf("CSP header missing style-src directive: %s", csp)
			}

			foundNonce := make(map[string]bool)
			cspParts := strings.Split(csp, ";")
			for _, cspPart := range cspParts {
				rawParts := strings.Split(strings.TrimSpace(cspPart), " ")
				directive := rawParts[0]
				for _, expression := range rawParts[1:] {
					if expression == "'nonce-"+testNonce+"'" {
						foundNonce[directive] = true
					}
				}
			}
			if _, hasScriptSrcNonce := foundNonce["script-src"]; !hasScriptSrcNonce {
				t.Errorf("script-src directive not correctly updated with nonce: %s", csp)
			}
			if _, hasStyleSrcNonce := foundNonce["style-src"]; !hasStyleSrcNonce {
				t.Errorf("style-src directive not correctly updated with nonce: %s", csp)
			}
		})
	}

	t.Run("ModifyResponseHeaders_ErrorNoNonceInScratch", func(t *testing.T) {
		modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{})
		ctx := FileModifierContext{
			Request: FileModifierContextRequest{Headers: make(http.Header)},
			Scratch: make(map[string]any), // Nonce is missing
		}

		_, err := modifier.ModifyResponseHeaders(ctx)
		if err == nil {
			t.Error("Expected error when nonce is missing from scratch context, but got nil")
		}
		if !strings.Contains(err.Error(), "nonce not in scratch") {
			t.Errorf("Expected error to contain 'nonce not in scratch', got: %v", err)
		}
	})

	t.Run("ModifyResponseHeaders_ErrorNonceNotString", func(t *testing.T) {
		modifier := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{})
		ctx := FileModifierContext{
			Request: FileModifierContextRequest{Headers: make(http.Header)},
			Scratch: map[string]any{"nonce": 123}, // Nonce is not a string
		}

		_, err := modifier.ModifyResponseHeaders(ctx)
		if err == nil {
			t.Error("Expected error when nonce in scratch is not a string, but got nil")
		}
		if !strings.Contains(err.Error(), "nonce is not a string") {
			t.Errorf("Expected error to contain 'nonce is not a string', got: %v", err)
		}
	})
}

// --- CompositeModifier Integration Test ---
// Test CSPContentNonceModifier combined with other modifiers via CompositeModifier
func TestCSPContentNonceModifier_CompositeIntegration(t *testing.T) {
	htmlInput := `<html><head><script>alert(1);</script></head><body><div style="color: blue;">{{NONCE_PLACEHOLDER}}</div></body></html>`

	// Modifier 1: Add a comment
	mod1 := &mockStringModifier{append: "<!-- Modified by Mod1 -->"}

	// Modifier 2: Add CSP nonce
	cspMod := NewCSPContentNonceModifier(CSPContentNonceModifierOptions{
		NonceStringReplacements: []string{"{{NONCE_PLACEHOLDER}}"}, // Placeholder for nonce in content
	})

	compositeModifier := NewCompositeModifier(mod1, cspMod)

	ctx := FileModifierContext{
		Request: FileModifierContextRequest{Headers: make(http.Header)},
		Scratch: make(map[string]any),
	}

	// First, test content modification
	modifiedContent, err := compositeModifier.ModifyContent(ctx, []byte(htmlInput))
	if err != nil {
		t.Fatalf("Composite ModifyContent failed: %v", err)
	}

	// Verify Mod1's change
	if !strings.Contains(string(modifiedContent), "<!-- Modified by Mod1 -->") {
		t.Errorf("Modified content missing Mod1's changes")
	}

	// Verify CSP Mod's content changes (nonce on script and inline style conversion)
	nonce := ctx.Scratch["nonce"].(string)
	if nonce == "" {
		t.Fatal("Nonce not found in scratch after content modification")
	}
	if !strings.Contains(string(modifiedContent), `<script nonce="`+nonce+`">`) {
		t.Errorf("Script tag in modified content missing nonce: %s", string(modifiedContent))
	}
	if !strings.Contains(string(modifiedContent), `<style nonce="`+nonce+`">`) { // The new style tag
		t.Errorf("New style tag for inline style in modified content missing nonce: %s", string(modifiedContent))
	}
	if strings.Contains(string(modifiedContent), `style="color: blue;"`) { // Original inline style removed
		t.Errorf("Original inline style not removed from div: %s", string(modifiedContent))
	}
	if strings.Contains(string(modifiedContent), "{{NONCE_PLACEHOLDER}}") {
		t.Error("Nonce placeholder was not replaced in content")
	}
	if !strings.Contains(string(modifiedContent), nonce) {
		t.Errorf("Generated nonce was not found in content after replacement: %s", string(modifiedContent))
	}

	// Test header modification
	modifiedHeaders, err := compositeModifier.ModifyResponseHeaders(ctx)
	if err != nil {
		t.Fatalf("Composite ModifyResponseHeaders failed: %v", err)
	}

	cspHeader := modifiedHeaders.Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("CSP header not found in modified headers")
	}

	// Verify nonce is in the CSP header
	extractedNonce, found := extractCSPNonce(cspHeader)
	if !found || extractedNonce != nonce {
		t.Errorf("CSP header nonce mismatch: expected %q, got %q (found: %t)", nonce, extractedNonce, found)
	}

	// Verify script-src and style-src are present
	if !strings.Contains(cspHeader, "script-src") {
		t.Errorf("CSP header missing script-src: %s", cspHeader)
	}
	if !strings.Contains(cspHeader, "style-src") {
		t.Errorf("CSP header missing style-src: %s", cspHeader)
	}
}

// compareStringSlices is a helper to compare two string slices for equality (order-sensitive).
func compareStringSlices(a, b []string) bool {
	if (a == nil) != (b == nil) {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
