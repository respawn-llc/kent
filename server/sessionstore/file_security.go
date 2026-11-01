package session

import (
	"errors"
	"fmt"
	"os"
)

var errSessionFileSymlink = errors.New("session file symlink")

func syncAndClose(fp *os.File, err error) error {
	if fp == nil {
		return errors.Join(err, os.ErrInvalid)
	}
	return errors.Join(err, fp.Sync(), fp.Close())
}

func openRegularSessionFile(path string, label string) (*os.File, error) {
	fp, err := openSessionFileReadOnly(path)
	if err != nil {
		if isSymlinkOpenError(err) {
			return nil, fmt.Errorf("%s: %w", label, ErrSessionFileSymlink)
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
