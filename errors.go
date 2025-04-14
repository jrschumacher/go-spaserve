package spaserve

import "errors"

// Pre-defined errors for modifier operations.
var ErrCouldNotMarshalEnv = errors.New("spaserve: could not marshal environment data to JSON")
var ErrCouldNotParseHtml = errors.New("spaserve: could not parse target file as HTML")
var ErrCouldNotFindHead = errors.New("spaserve: could not find <head> tag in HTML")
var ErrCouldNotRenderHtml = errors.New("spaserve: could not render modified HTML")
var ErrFileNotFound = errors.New("spaserve: source file not found in fs")

// Pre-defined errors for configuration.
var ErrMissingFS = errors.New("spaserve: configuration must include a source FS")
var ErrInvalidBasePath = errors.New("spaserve: base path must start with '/'")
var ErrInvalidNamespace = errors.New("spaserve: HTML script namespace must be a valid JavaScript identifier")
var ErrModifierFailed = errors.New("spaserve: file modification failed")
var ErrConfigTargetMissing = errors.New("spaserve: target config missing modifier or target file path")

// injectWebEnv.InjectWindowVars
var ErrCouldNotMarshalConfig = errors.New("spaserve: could not marshal config")
var ErrNoIndexFound = errors.New("spaserve: no index.html found")
var ErrUnexpectedWalkError = errors.New("spaserve: unexpected walk error")
var ErrCouldNotOpenFile = errors.New("spaserve: could not open file")
var ErrCouldNotReadFile = errors.New("spaserve: could not read file")
var ErrCouldNotAppendToIndex = errors.New("spaserve: could not append to index")
var ErrCouldNotMakeDir = errors.New("spaserve: could not make dir")
var ErrCouldNotWriteFile = errors.New("spaserve: could not write file")
var ErrCouldNotParseNamespace = errors.New("spaserve: namespace must match regex: ^[a-zA-Z_][a-zA-Z0-9_]*$")
var ErrNoNamespace = errors.New("spaserve: no namespace provided")

// injectWebEnv.appendToIndex
var ErrCouldNotParseIndex = errors.New("spaserve: could not parse index")
var ErrCouldNotAppendScript = errors.New("spaserve: could not append script")
var ErrCouldNotWriteIndex = errors.New("spaserve: could not write index")
