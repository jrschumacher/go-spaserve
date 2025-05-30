package spaserve

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// --- Composite Modifier ---

// CompositeModifier applies a list of modifiers in sequence.
type CompositeModifier struct {
	modifiers []FileModifier
}

// NewCompositeModifier creates a modifier that chains others.
// It's recommended to provide a logger via config or it will default.
func NewCompositeModifier(modifiers ...FileModifier) *CompositeModifier {
	return &CompositeModifier{modifiers: modifiers}
}

func (cm *CompositeModifier) ModifyContent(context FileModifierContext, content []byte) ([]byte, error) {
	currentContent := content
	var err error
	for i, modifier := range cm.modifiers {
		if modifier == nil {
			continue
		}
		fileContentModifier, isFileContentModifier := modifier.(FileContentModifier)
		if !isFileContentModifier {
			continue
		}
		currentContent, err = fileContentModifier.ModifyContent(context, currentContent)
		if err != nil {
			// Wrap the error for context
			return nil, fmt.Errorf("composite modifier step %d (ModifyContent) failed for path %s: %w", i, context.Request.Path, err)
		}
	}
	return currentContent, nil
}

func (cm *CompositeModifier) ModifyResponseHeaders(context FileModifierContext) (http.Header, error) {
	currentResponseHeaders := context.Request.Headers
	var err error
	for i, modifier := range cm.modifiers {
		if modifier == nil {
			continue
		}
		fileResponseHeadersModifier, isFileResponseHeadersModifier := modifier.(FileResponseHeaderModifier)
		if !isFileResponseHeadersModifier {
			continue
		}
		currentResponseHeaders, err = fileResponseHeadersModifier.ModifyResponseHeaders(context)
		if err != nil {
			// Wrap the error for context
			return nil, fmt.Errorf("composite modifier step %d (ModifyResponseHeaders) failed for path %s: %w", i, context.Request.Path, err)
		}

		currentResponseHeadersClone := currentResponseHeaders.Clone()

		context.Request.Headers = currentResponseHeadersClone
	}
	return currentResponseHeaders, nil
}

// Ensure CompositeModifier implements the interface (compile-time check)
var _ FileContentModifier = (*CompositeModifier)(nil)
var _ FileResponseHeaderModifier = (*CompositeModifier)(nil)

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

func (hsm *HtmlScriptTagEnvModifier) ModifyContent(context FileModifierContext, originalContent []byte) ([]byte, error) {
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
var _ FileContentModifier = (*HtmlScriptTagEnvModifier)(nil)

// CSPResponseHeaderModifier allows injecting a Content-Security-Policy header
type CSPResponseHeaderModifier struct {
	getCsp func(context FileModifierContext) (string, error) // The CSP to apply
}

// NewCSPResponseHeaderModifier creates a FileModifier that applies CSP to a response.
func NewCSPResponseHeaderModifier(getCsp func(context FileModifierContext) (string, error)) *CSPResponseHeaderModifier {
	return &CSPResponseHeaderModifier{
		getCsp: getCsp,
	}
}

func (csp *CSPResponseHeaderModifier) ModifyResponseHeaders(context FileModifierContext) (http.Header, error) {
	newCSP := make(map[string][]string)
	for _, existingCSPHeader := range context.Request.Headers.Values("Content-Security-Policy") {
		rawParts := strings.Split(existingCSPHeader, ";")
		for _, rawPart := range rawParts {
			rawDirectiveAndSourceExpressions := strings.Split(strings.TrimSpace(rawPart), " ")
			directive := strings.TrimSpace(rawDirectiveAndSourceExpressions[0])
			if directive != "" {
				for _, rawSourceExpression := range rawDirectiveAndSourceExpressions[1:] {
					sourceExpression := strings.TrimSpace(rawSourceExpression)
					if sourceExpression != "" {
						newCSP[directive] = append(newCSP[directive], sourceExpression)
					}
				}
			}
		}
	}
	userCSP, err := csp.getCsp(context)
	if err != nil {
		return nil, err
	}
	rawParts := strings.Split(userCSP, ";")
	for _, rawPart := range rawParts {
		rawDirectiveAndSourceExpressions := strings.Split(strings.TrimSpace(rawPart), " ")
		directive := strings.TrimSpace(rawDirectiveAndSourceExpressions[0])
		if directive != "" {
			for _, rawSourceExpression := range rawDirectiveAndSourceExpressions[1:] {
				sourceExpression := strings.TrimSpace(rawSourceExpression)
				if sourceExpression != "" {
					newCSP[directive] = append(newCSP[directive], sourceExpression)
				}
			}
		}
	}
	newCSPHeaderParts := make([]string, 0)
	for directive, sourceExpressions := range newCSP {
		joinedSourceExpressions := strings.Join(sourceExpressions, " ")
		newCSPHeaderParts = append(newCSPHeaderParts, directive+" "+joinedSourceExpressions)
	}
	modifiedHeaders := context.Request.Headers.Clone()
	modifiedHeaders.Del("Content-Security-Policy")
	modifiedHeaders.Add("Content-Security-Policy", strings.Join(newCSPHeaderParts, "; "))
	return modifiedHeaders, nil
}

// Ensure HtmlScriptTagEnvModifier implements the interface (compile-time check)
var _ FileResponseHeaderModifier = (*CSPResponseHeaderModifier)(nil)

type NonceElementDecider interface {
	// ShouldModify returns true if the node (a valid nonce target) should receive the nonce
	ShouldModify(node *html.Node) bool
}

type CSPContentNonceModifierOptions struct {
	// NonceElementDecider allows targeted inclusion/exclusion of particular nodes
	NonceElementDecider NonceElementDecider
	// NonceLength is the number of bytes underlying the actual nonce value
	NonceLength int
	// NonceStringReplacements is a list of strings that will be directly replaced with the nonce (unescaped) everywhere in the content
	NonceStringReplacements []string
}

// CSPContentNonceModifier allows injecting a Content-Security-Policy header
type CSPContentNonceModifier struct {
	nonceElementDecider     NonceElementDecider
	nonceLength             int
	nonceStringReplacements []string
}

// NewCSPContentNonceModifier creates a FileContentModifier that applies CSP to a response.
func NewCSPContentNonceModifier(options CSPContentNonceModifierOptions) *CSPContentNonceModifier {
	return &CSPContentNonceModifier{
		nonceElementDecider:     options.NonceElementDecider,
		nonceLength:             options.NonceLength,
		nonceStringReplacements: options.NonceStringReplacements,
	}
}

func attrListHas(attrList []html.Attribute, key string, value string) bool {
	for _, attr := range attrList {
		if attr.Key == key && attr.Val == value {
			return true
		}
	}
	return false
}

func getAttr(attrList []html.Attribute, key string) (html.Attribute, bool) {
	for _, attr := range attrList {
		if attr.Key == key {
			return attr, true
		}
	}
	return html.Attribute{}, false
}

func (csp *CSPContentNonceModifier) applyNonceToNodes(node *html.Node, nonce string) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		csp.applyNonceToNodes(child, nonce)
	}
	if node.Type != html.ElementNode {
		return
	}
	canHaveNonce := node.Data == "script" || node.Data == "style" || (node.Data == "link" && attrListHas(node.Attr, "rel", "stylesheet"))
	styleAttr, hasStyleAttr := getAttr(node.Attr, "style")
	if canHaveNonce || hasStyleAttr {
		if csp.nonceElementDecider != nil && !csp.nonceElementDecider.ShouldModify(node) {
			return
		}
		switch {
		case canHaveNonce:
			node.Attr = append(node.Attr, html.Attribute{
				Key: "nonce",
				Val: nonce,
			})
		case hasStyleAttr:
			parent := node.Parent
			if parent == nil {
				return
			}

			potentialClassName, err := makeNonce(defaultNonceLength)
			if err != nil {
				return
			}
			className := "_" + potentialClassName
			styleContentNode := &html.Node{
				Type: html.TextNode,
				Data: "." + className + "{" + styleAttr.Val + "}",
			}
			styleNode := &html.Node{
				Type: html.ElementNode,
				Data: "style",
				Attr: []html.Attribute{
					{
						Key: "nonce",
						Val: nonce,
					},
				},
				FirstChild: styleContentNode,
				LastChild:  styleContentNode,
			}
			parent.InsertBefore(styleNode, node)

			classAttr, hasClassAttr := getAttr(node.Attr, "class")
			newClassAttr := className
			if hasClassAttr {
				newClassAttr = classAttr.Val + " " + className
			}
			var newAttrs []html.Attribute
			for _, attr := range node.Attr {
				if attr.Key == "class" && hasClassAttr {
					attr.Val = classAttr.Val + " " + className
				}
				if attr.Key != "style" && attr.Key != "class" {
					newAttrs = append(newAttrs, attr)
				}
			}
			node.Attr = append(newAttrs, html.Attribute{
				Key: "class",
				Val: newClassAttr,
			})
		}
	}
}

func makeNonce(length int) (string, error) {
	randomBytes := make([]byte, length)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("error generating random bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

const defaultNonceLength = 10

func (csp *CSPContentNonceModifier) ModifyContent(context FileModifierContext, content []byte) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCouldNotParseHtml, err)
	}
	nonceLength := csp.nonceLength
	if nonceLength == 0 {
		nonceLength = defaultNonceLength
	}
	nonce, err := makeNonce(nonceLength)
	if err != nil {
		return nil, err
	}
	context.Scratch["nonce"] = nonce
	csp.applyNonceToNodes(doc, nonce)
	var buffer bytes.Buffer
	err = html.Render(&buffer, doc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCouldNotRenderHtml, err)
	}

	if csp.nonceStringReplacements == nil {
		return buffer.Bytes(), nil
	}

	replacedResult := buffer.String()
	for _, replacement := range csp.nonceStringReplacements {
		replacedResult = strings.ReplaceAll(replacedResult, replacement, nonce)
	}
	return []byte(replacedResult), nil
}

func (csp *CSPContentNonceModifier) ModifyResponseHeaders(context FileModifierContext) (http.Header, error) {
	savedNonce, hasSavedNonce := context.Scratch["nonce"]
	if !hasSavedNonce {
		return nil, fmt.Errorf("CSPContentNonceModifier.ModifyResponseHeaders: nonce not in scratch")
	}
	nonce, nonceIsString := savedNonce.(string)
	if !nonceIsString {
		return nil, fmt.Errorf("CSPContentNonceModifier.ModifyResponseHeaders: nonce is not a string: %q", nonce)
	}
	newCSP := make(map[string][]string)
	for _, existingCSPHeader := range context.Request.Headers.Values("Content-Security-Policy") {
		rawParts := strings.Split(existingCSPHeader, ";")
		for _, rawPart := range rawParts {
			rawDirectiveAndSourceExpressions := strings.Split(strings.TrimSpace(rawPart), " ")
			directive := strings.TrimSpace(rawDirectiveAndSourceExpressions[0])
			if directive != "" {
				for _, rawSourceExpression := range rawDirectiveAndSourceExpressions[1:] {
					sourceExpression := strings.TrimSpace(rawSourceExpression)
					if sourceExpression != "" {
						newCSP[directive] = append(newCSP[directive], sourceExpression)
					}
				}
			}
		}
	}
	nonceSourceExpression := fmt.Sprintf("'nonce-%s'", nonce)
	newCSP["script-src"] = append(newCSP["script-src"], nonceSourceExpression)
	newCSP["style-src"] = append(newCSP["style-src"], nonceSourceExpression)
	newCSPHeaderParts := make([]string, 0)
	for directive, sourceExpressions := range newCSP {
		joinedSourceExpressions := strings.Join(sourceExpressions, " ")
		newCSPHeaderParts = append(newCSPHeaderParts, directive+" "+joinedSourceExpressions)
	}
	modifiedHeaders := context.Request.Headers.Clone()
	modifiedHeaders.Del("Content-Security-Policy")
	modifiedHeaders.Add("Content-Security-Policy", strings.Join(newCSPHeaderParts, "; "))
	return modifiedHeaders, nil
}

// Ensure CSPContentNonceModifier implements the interface (compile-time check)
var _ FileContentModifier = (*CSPContentNonceModifier)(nil)
var _ FileResponseHeaderModifier = (*CSPContentNonceModifier)(nil)
