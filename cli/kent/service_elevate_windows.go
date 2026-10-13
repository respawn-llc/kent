//go:build windows

package main

import (
	brand "core/shared/config"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32DLL         = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteEx = shell32DLL.NewProc("ShellExecuteExW")
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swShowNormal          = 1
)

type shellExecuteInfo struct {
	cbSize        uint32
	mask          uint32
	hwnd          uintptr
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       uintptr
	idList        uintptr
	class         *uint16
	hkeyClass     uintptr
	hotKey        uint32
	iconOrMonitor uintptr
	process       windows.Handle
}

func keepRunningUninstallSupported() bool {
	return false
}

func elevateServiceAction(action serviceAction) (int, bool) {
	switch action {
	case serviceActionInstall, serviceActionUninstall, serviceActionRestart:
	default:
		return 0, false
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		return 0, false
	}
	args, err := elevatedServiceParams()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	output, err := captureElevatedServiceOutput()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	defer func() {
		if err := output.close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	paramsPtr, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(output.commandArgs(args)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoCloseProcess,
		verb:       verbPtr,
		file:       exePtr,
		parameters: paramsPtr,
		show:       swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	ret, _, callErr := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		fmt.Fprintf(os.Stderr, "elevation was declined or failed; run `%s service %s` from an Administrator terminal: %v\n", brand.Command, action, callErr)
		return 1, true
	}
	if info.process == 0 {
		fmt.Fprintln(os.Stderr, "elevation returned no process handle; service action completion is unknown")
		return 1, true
	}
	defer func() { _ = windows.CloseHandle(info.process) }()
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.process, &code); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	if err := output.replay(os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1, true
	}
	if code != 0 {
		fmt.Fprintf(os.Stderr, "elevated service %s failed with exit status %d\n", action, code)
	}
	return int(code), true
}

func elevatedServiceParams() ([]string, error) {
	args := os.Args[1:]
	root := strings.TrimSpace(os.Getenv(brand.PersistenceRootEnvName))
	if root == "" {
		root = brand.DefaultPersistence
	}
	abs, err := brand.NormalizePersistenceRoot(root)
	if err != nil {
		return nil, fmt.Errorf("resolve invoking user's persistence root before elevation: %w", err)
	}
	return append(argsWithoutPersistenceRoot(args), "--persistence-root", abs), nil
}

// ShellExecuteEx cannot inherit redirected standard handles across UAC.
// Capture the elevated child's output and relay it once the child exits.
const elevatedServiceOutputFlag = "--service-elevation-output"

type elevatedServiceOutput struct {
	stdout *os.File
	stderr *os.File
}

func captureElevatedServiceOutput() (*elevatedServiceOutput, error) {
	stdout, err := os.CreateTemp("", "kent-service-stdout-*")
	if err != nil {
		return nil, fmt.Errorf("capture elevated service stdout: %w", err)
	}
	stderr, err := os.CreateTemp("", "kent-service-stderr-*")
	if err != nil {
		return nil, errors.Join(fmt.Errorf("capture elevated service stderr: %w", err), stdout.Close(), os.Remove(stdout.Name()))
	}
	return &elevatedServiceOutput{stdout: stdout, stderr: stderr}, nil
}

func (o *elevatedServiceOutput) commandArgs(args []string) []string {
	return append([]string{elevatedServiceOutputFlag, o.stdout.Name(), o.stderr.Name()}, args...)
}

func (o *elevatedServiceOutput) close() error {
	return errors.Join(o.stdout.Close(), o.stderr.Close(), os.Remove(o.stdout.Name()), os.Remove(o.stderr.Name()))
}

func (o *elevatedServiceOutput) replay(stdout, stderr io.Writer) error {
	_, outErr := io.Copy(stdout, o.stdout)
	_, errErr := io.Copy(stderr, o.stderr)
	return errors.Join(outErr, errErr)
}

func openElevatedServiceOutput(args []string) ([]string, *os.File, *os.File, error) {
	if len(args) == 0 || args[0] != elevatedServiceOutputFlag {
		return args, nil, nil, nil
	}
	if len(args) < 5 || args[3] != "service" {
		return nil, nil, nil, errors.New("invalid elevated service invocation")
	}
	switch serviceAction(args[4]) {
	case serviceActionInstall, serviceActionUninstall, serviceActionRestart:
	default:
		return nil, nil, nil, errors.New("invalid elevated service action")
	}
	stdout, err := os.OpenFile(args[1], os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open elevated service stdout: %w", err)
	}
	stderr, err := os.OpenFile(args[2], os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, nil, nil, errors.Join(fmt.Errorf("open elevated service stderr: %w", err), stdout.Close())
	}
	return args[3:], stdout, stderr, nil
}
