package main

import (
	"errors"
	"flag"
	"io"
)

func newCommandFlagSet(name string, stderr io.Writer, usage commandUsage) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage.write(fs) }
	return fs
}

func parseCommandFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}
