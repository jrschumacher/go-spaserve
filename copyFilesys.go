package spaserve

import (
	"errors"
	"io"
	"io/fs"

	"github.com/psanford/memfs"
)

// OnHookFunc is a function that can be used to modify the data of a file before it is written to the memfs.
// The function should return the modified data and an error if one occurred.
type OnHookFunc func(path string, data []byte) ([]byte, error)

func CopyFileSys(filesys fs.FS, onHook OnHookFunc) (*memfs.FS, error) {
	mfs := memfs.New()
	err := fs.WalkDir(filesys, ".", func(path string, d fs.DirEntry, err error) error {
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

		// run onHook
		if onHook != nil {
			data, err = onHook(path, data)
			if err != nil {
				return err
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
