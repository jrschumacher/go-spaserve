package spaserve

import (
	"bytes"
	"errors"
	"testing"

	"github.com/psanford/memfs"
	"golang.org/x/net/html"
)

type testInjectWebEnvFixtures struct {
	goodHtml       []byte
	goodHtmlNode   *html.Node
	htmlNoHead     []byte
	htmlNoHeadNode *html.Node
}

func testInjectWebEnvSetup() testInjectWebEnvFixtures {
	goodHtml := []byte("<html><head><title /></head><body></body></html>")
	goodHtmlNode, err := html.Parse(bytes.NewReader(goodHtml))
	if err != nil {
		panic(err)
	}

	htmlNoHead := []byte("<html><body></body></html>")
	htmlNoHeadNode, err := html.Parse(bytes.NewReader(htmlNoHead))
	if err != nil {
		panic(err)
	}

	return testInjectWebEnvFixtures{
		goodHtml:       goodHtml,
		goodHtmlNode:   goodHtmlNode,
		htmlNoHead:     htmlNoHead,
		htmlNoHeadNode: htmlNoHeadNode,
	}
}

func TestInjectWebEnv(t *testing.T) {
	// Create a mock file system
	fsys := memfs.New()
	_ = fsys.MkdirAll(".", 0755)
	_ = fsys.WriteFile("index.html", []byte("<html><head></head><body></body></html>"), 0644)

	// Define the web environment configuration
	conf := struct {
		Foo string `json:"foo"`
		Bar int    `json:"bar"`
	}{
		Foo: "hello",
		Bar: 42,
	}

	// Define the namespace
	ns := "myNamespace"

	// Call the InjectWebEnv function
	result, err := InjectWebEnv(fsys, conf, ns)

	// Check for errors
	if err != nil {
		t.Errorf("InjectWebEnv returned an unexpected error: %v", err)
	}

	// Check if the index.html file exists in the result file system
	if !indexExists(result) {
		t.Error("InjectWebEnv did not inject the web environment into the index.html file")
	}

	// Check if ns is a valid namespace
	if _, err = InjectWebEnv(fsys, conf, ""); !errors.Is(err, ErrNoNamespace) {
		t.Errorf("InjectWebEnv did not return ErrNoNamespace: %v", err)
	}

	if _, err = InjectWebEnv(fsys, conf, "!!!!"); !errors.Is(err, ErrCouldNotParseNamespace) {
		t.Errorf("InjectWebEnv did not return ErrCouldNotParseNamespace: %v", err)
	}

	// Check if index file exists
	if _, err = InjectWebEnv(memfs.New(), conf, ns); !errors.Is(err, ErrNoIndexFound) {
		t.Errorf("InjectWebEnv did not return ErrNoIndexFound: %v", err)
	}
}
func TestAppendToIndex(t *testing.T) {
	scriptNode := html.Node{
		Type: html.ElementNode,
		Data: "script",
		Attr: []html.Attribute{
			{Key: "type", Val: "text/javascript"},
		},
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: "window.webEnv = {\"foo\":\"hello\",\"bar\":42};",
		},
	}

	var data bytes.Buffer
	if err := html.Render(&data, &html.Node{
		Type: html.ElementNode,
		Data: "html",
		FirstChild: &html.Node{
			Type: html.ElementNode,
			Data: "head",
		},
		LastChild: &html.Node{
			Type: html.ElementNode,
			Data: "body",
		},
	}); err != nil {
		t.Fatalf("html.Render() returned an unexpected error: %v", err)
	}

	s := scriptNode
	var want bytes.Buffer
	if err := html.Render(&want, &html.Node{
		Type: html.ElementNode,
		Data: "html",
		FirstChild: &html.Node{
			Type:       html.ElementNode,
			Data:       "head",
			FirstChild: &s,
			NextSibling: &html.Node{
				Type: html.ElementNode,
				Data: "body",
			},
		},
	}); err != nil {
		t.Fatalf("html.Render() returned an unexpected error: %v", err)
	}

	tt := []struct {
		name string
		path string
		data []byte
		want []byte
	}{
		{
			name: "when path is not index.html should return data",
			path: "not-index.html",
			data: []byte("Hello, World!"),
			want: []byte("Hello, World!"),
		},
		{
			name: "when path is index.html should return data with script tag",
			path: "index.html",
			data: data.Bytes(),
			want: want.Bytes(),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := appendToIndex(&scriptNode)(tc.path, tc.data); err != nil {
				t.Errorf("appendToIndex() error = %v, want nil", err)
			} else if !bytes.Equal(got, tc.want) {
				t.Errorf("appendToIndex() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestFindHead(t *testing.T) {
	headNode := &html.Node{
		Type: html.ElementNode,
		Data: "head",
	}

	tt := []struct {
		name string
		node *html.Node
		want *html.Node
	}{
		{
			name: "when head first should return node",
			node: &html.Node{
				Type:       html.ElementNode,
				Data:       "html",
				FirstChild: headNode,
				LastChild: &html.Node{
					Type: html.ElementNode,
					Data: "body",
				},
			},
			want: headNode,
		},
		{
			name: "when body first should return nil",
			node: &html.Node{
				Type: html.ElementNode,
				Data: "html",
				FirstChild: &html.Node{
					Type: html.ElementNode,
					Data: "body",
				},
				LastChild: headNode,
			},
			want: nil,
		},
		{
			name: "when no head or body should return nil",
			node: &html.Node{
				Type: html.ElementNode,
				Data: "html",
				FirstChild: &html.Node{
					Type: html.ElementNode,
					Data: "div",
				},
				LastChild: &html.Node{
					Type: html.ElementNode,
					Data: "div",
				},
			},
			want: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if got := findHead(tc.node); got != tc.want {
				t.Errorf("findHead() = %v, want %v", got, tc.want)
			}
		})
	}
}
func TestIndexExists(t *testing.T) {
	fsWithIndex := memfs.New()
	_ = fsWithIndex.MkdirAll(".", 0755)
	_ = fsWithIndex.WriteFile("index.html", []byte("Hello, World!"), 0644)

	fsWithoutIndex := memfs.New()
	_ = fsWithoutIndex.MkdirAll(".", 0755)

	t.Run("index exists", func(t *testing.T) {
		exists := indexExists(fsWithIndex)
		if !exists {
			t.Errorf("indexExists() = false, want true")
		}
	})

	t.Run("index does not exist", func(t *testing.T) {
		exists := indexExists(fsWithoutIndex)
		if exists {
			t.Errorf("indexExists() = true, want false")
		}
	})
}
func TestConstructScriptTag(t *testing.T) {
	conf := struct {
		Foo string `json:"foo"`
		Bar int    `json:"bar"`
	}{
		Foo: "hello",
		Bar: 42,
	}

	ns := "myNamespace"

	want := `<script type="text/javascript">window.myNamespace = {"foo":"hello","bar":42};</script>`

	scriptTag, err := constructScriptTag(ns, conf)
	if err != nil {
		t.Fatalf("constructScriptTag() returned an unexpected error: %v", err)
	}

	var buf bytes.Buffer
	if err := html.Render(&buf, scriptTag); err != nil {
		t.Fatalf("html.Render() returned an unexpected error: %v", err)
	}

	got := buf.String()
	if got != want {
		t.Errorf("constructScriptTag() = %s, want %s", got, want)
	}
}
func TestConstructScriptTag_MarshalError(t *testing.T) {

	ns := "myNamespace"

	wantErr := ErrCouldNotMarshalConfig

	_, err := constructScriptTag(ns, make(chan int))
	if err == nil {
		t.Fatal("constructScriptTag() did not return an error")
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("constructScriptTag() error = %v, want %v", err, wantErr)
	}
}
