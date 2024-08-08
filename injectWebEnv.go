package spaserve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/psanford/memfs"
	"golang.org/x/net/html"
)

var namespaceRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// InjectWebEnv injects the web environment into the index.html file of the given file system.
//   - filesys: the file system to inject the web environment into
//   - conf: the web environment to inject, use json struct tags to drive the marshalling
//   - ns: the namespace to use for the web environment, must match regex: ^[a-zA-Z_][a-zA-Z0-9_]*$
func InjectWebEnv(filesys fs.FS, conf any, ns string) (*memfs.FS, error) {
	if ns == "" {
		return nil, ErrNoNamespace
	}
	ns = strings.TrimSpace(ns)
	if !namespaceRegex.Match([]byte(ns)) {
		return nil, ErrCouldNotParseNamespace
	}

	if !indexExists(filesys) {
		return nil, ErrNoIndexFound
	}

	scriptTag, err := constructScriptTag(ns, conf)
	if err != nil {
		return nil, err
	}

	return CopyFileSys(filesys, appendToIndex(scriptTag))
}

// indexExists returns true if the index.html file exists in the given file system
func indexExists(filesys fs.FS) bool {
	indexFile := path.Join(".", "index.html")
	_, err := filesys.Open(indexFile)
	return err == nil
}

// constructScriptTag constructs a script tag with the given namespace and configuration
func constructScriptTag(ns string, conf any) (*html.Node, error) {
	b, err := json.Marshal(conf)
	if err != nil {
		return nil, errors.Join(ErrCouldNotMarshalConfig, err)
	}

	return &html.Node{
		Type: html.ElementNode,
		Data: "script",
		Attr: []html.Attribute{{Key: "type", Val: "text/javascript"}},
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: "window." + ns + " = " + string(b) + ";",
		},
	}, nil
}

// appendToIndex returns a function that appends a script tag to the head of the index.html file
func appendToIndex(t *html.Node) func(string, []byte) ([]byte, error) {
	return func(p string, d []byte) ([]byte, error) {
		// skip if not root index.html
		if p != "index.html" {
			return d, nil
		}

		// parse index.html
		doc, err := html.Parse(bytes.NewReader(d))
		if err != nil {
			return []byte{}, errors.Join(ErrCouldNotParseIndex, err)
		}

		// find head tag
		headTag := findHead(doc)
		if headTag == nil {
			return []byte{}, ErrCouldNotFindHead
		}

		// insert script before first child of head
		headTag.InsertBefore(t, headTag.FirstChild)

		// render doc to bytes
		var b bytes.Buffer
		if err := html.Render(&b, doc); err != nil {
			return []byte{}, errors.Join(ErrCouldNotWriteIndex, err)
		}
		return b.Bytes(), nil
	}
}

// findHead recursively searches for the head tag in the html document
func findHead(n *html.Node) *html.Node {
	// check if node is body tag and return nil
	if n.Type == html.ElementNode && n.Data == "body" {
		return nil
	}

	// check if node is head tag
	if n.Type == html.ElementNode && n.Data == "head" {
		return n
	}

	// recursively search for head tag
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if head := findHead(c); head != nil {
			return head
		}
	}

	// head tag not found
	return nil
}
