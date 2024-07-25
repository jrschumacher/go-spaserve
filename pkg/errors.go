package pkg

import "errors"

// injectWebEnv.InjectWindowVars
var ErrCouldNotMarshalConfig = errors.New("could not marshal config")
var ErrNoIndexFound = errors.New("no index.html found")
var ErrUnexpectedWalkError = errors.New("unexpected walk error")
var ErrCouldNotOpenFile = errors.New("could not open file")
var ErrCouldNotReadFile = errors.New("could not read file")
var ErrCouldNotAppendToIndex = errors.New("could not append to index")
var ErrCouldNotMakeDir = errors.New("could not make dir")
var ErrCouldNotWriteFile = errors.New("could not write file")

// injectWebEnv.appendToIndex
var ErrCouldNotParseIndex = errors.New("could not parse index")
var ErrCouldNotFindHead = errors.New("could not find <head> tag")
var ErrCouldNotAppendScript = errors.New("could not append script")
var ErrCouldNotWriteIndex = errors.New("could not write index")
