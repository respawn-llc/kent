package session

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var errSessionFileSymlink = errors.New("session file symlink")

func readRegularSessionFile(path string, label string) ([]byte, error) {
	fp, err := openRegularSessionFile(path, label)
	if err != nil {
		return nil, err
	}
	defer fp.Close()
	data, err := io.ReadAll(fp)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func openRegularSessionFile(path string, label string) (*os.File, error) {
	fp, err := openSessionFileReadOnly(path)
	if err != nil {
		if isSymlinkOpenError(err) {
			return nil, fmt.Errorf("%s must not be a symlink", label)
		}
		return nil, err
	}
	info, err := fp.Stat()
	if err != nil {
		_ = fp.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = fp.Close()
		return nil, fmt.Errorf("%s must be a regular file", label)
	}
	return fp, nil
}
