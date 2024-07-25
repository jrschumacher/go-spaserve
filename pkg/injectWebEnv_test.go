package pkg

import (
	"bytes"
	"errors"
	"testing"

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
}

func TestInjectWebEnv_appendToIndex(t *testing.T) {
	htmlText := "<html><head><title></title></head><body></body></html>"
	appendNode := &html.Node{
		Type: html.ElementNode,
		Data: "injected",
	}

	tt := []struct {
		name       string
		indexHtml  string
		appendNode *html.Node
		want       string
		errorType  error
	}{
		// TODO: figure out how to test this
		//       html.Parse is very forgiving and seems to parse anything
		// {
		// 	name:       "could not parse index",
		// 	indexHtml:  "bad html",
		// 	appendNode: appendNode,
		// 	want:       "",
		// 	errorType:  ErrCouldNotParseIndex,
		// },
		// TODO: figure out how to test this
		//       html.Parse adds head
		// {
		// 	name:       "could not find <head> tag",
		// 	indexHtml:  "<html><body></body></html>",
		// 	appendNode: appendNode,
		// 	want:       "",
		// 	errorType:  ErrCouldNotFindHead,
		// },
		// {
		// 	name:       "could not append script",
		// 	indexHtml:  htmlText,
		// 	appendNode: appendNode,
		// 	want:       "",
		// 	errorType:  ErrCouldNotAppendScript,
		// },
		// {
		// 	name:       "could not write index",
		// 	indexHtml:  htmlText,
		// 	appendNode: appendNode,
		// 	want:       "",
		// 	errorType:  ErrCouldNotWriteIndex,
		// },
		{
			name:       "should append node",
			indexHtml:  htmlText,
			appendNode: appendNode,
			want:       "<html><head><injected></injected><title></title></head><body></body></html>",
			errorType:  nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := appendToIndex([]byte(tc.indexHtml), tc.appendNode); err != tc.errorType {
				if tc.errorType != nil {
					if !errors.Is(err, tc.errorType) {
						t.Errorf("appendToIndex() error = %v, want %v", err, tc.errorType)
					}
				} else if string(got) != tc.want {
					t.Errorf("appendToIndex() = %v, want %v", string(got), tc.want)
				}
			}
		})
	}
}

func TestInjectWebEnv_findHead(t *testing.T) {
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
