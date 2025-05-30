package spaserve

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
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
