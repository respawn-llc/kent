//go:build windows

package main

import (
	brand "core/shared/config"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const (
	stdoutLogEnvVar = brand.EnvPrefix + "SERVICE_STDOUT_LOG"
	stderrLogEnvVar = brand.EnvPrefix + "SERVICE_STDERR_LOG"
)

func redirectServiceLogs() {
	args, stdout, stderr, err := openElevatedServiceOutput(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if stdout != nil {
		os.Args = append([]string{os.Args[0]}, args...)
		os.Stdout, os.Stderr = stdout, stderr
		if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(stdout.Fd())); err != nil {
			fmt.Fprintln(stderr, err)
			os.Exit(1)
		}
		if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(stderr.Fd())); err != nil {
			fmt.Fprintln(stderr, err)
			os.Exit(1)
		}
		return
	}
	redirectStdStream(stdoutLogEnvVar, &os.Stdout, windows.STD_OUTPUT_HANDLE)
	redirectStdStream(stderrLogEnvVar, &os.Stderr, windows.STD_ERROR_HANDLE)
}

func redirectStdStream(envVar string, target **os.File, stdHandle uint32) {
	path := os.Getenv(envVar)

	_ = os.Unsetenv(envVar)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	*target = f
	_ = windows.SetStdHandle(stdHandle, windows.Handle(f.Fd()))
}
