package pkg

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/psanford/memfs"
	"golang.org/x/net/html"
)

type UI struct {
	FS              fs.FS
	GlobalNamespace string
}

const defaultGlobalNamespace = "APP_ENV"

func InjectWindowVars(conf any, filesys fs.FS, ns string) (*memfs.FS, error) {
	ns = strings.TrimSpace(ns)

	b, err := json.Marshal(conf)
	if err != nil {
		return nil, errors.Join(ErrCouldNotMarshalConfig, err)
	}

	// create script tag
	scriptTag := &html.Node{
		Type: html.ElementNode,
		Data: "script",
		Attr: []html.Attribute{{Key: "type", Val: "text/javascript"}},
	}
	scriptTag.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: "window." + ns + " = " + string(b) + ";",
	})

	// check if index.html exists
	indexFile := path.Join(".", "index.html")
	if _, err := filesys.Open(indexFile); err != nil {
		return nil, errors.Join(ErrNoIndexFound, err)
	}

	// create memfs and walk staticFs
	mfs := memfs.New()
	err = fs.WalkDir(filesys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Join(ErrUnexpectedWalkError, err)
		}

		// create dir and continue
		if d.IsDir() {
			if err := mfs.MkdirAll(path, 0o755); err != nil {
				return errors.Join(ErrCouldNotMakeDir, err)
			}
			return nil
		}

		// open file
		f, err := filesys.Open(path)
		if err != nil {
			return errors.Join(ErrCouldNotOpenFile, err)
		}
		defer f.Close()

		// read file
		data, err := io.ReadAll(f)
		if err != nil {
			return errors.Join(ErrCouldNotReadFile, err)
		}

		// append script tag to index.html
		if path == "index.html" {
			data, err = appendToIndex(data, scriptTag)
			if err != nil {
				return errors.Join(ErrCouldNotAppendToIndex, err)
			}
		}

		// write file to memfs
		if err := mfs.WriteFile(path, data, fs.ModeAppend); err != nil {
			return errors.Join(ErrCouldNotWriteFile, err)
		}

		return nil
	})

	return mfs, err
}

func appendToIndex(d []byte, t *html.Node) ([]byte, error) {
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
