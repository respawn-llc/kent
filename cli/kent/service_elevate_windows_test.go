//go:build windows

package main

import (
	"bytes"
	brand "core/shared/config"
	"errors"
	"os"
	"slices"
	"testing"
)

func TestElevatedServiceOutputReachesInvokingProcess(t *testing.T) {
	output, err := captureElevatedServiceOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.close(); err != nil {
			t.Error(err)
		}
	})
	original := []string{"service", "install", "--persistence-root", `C:\Users\Nek\.kent`}
	args, stdout, stderr, err := openElevatedServiceOutput(output.commandArgs(original))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, original) {
		t.Fatalf("service arguments changed: %v", args)
	}
	out := []byte{1, 2, 3, '\n'}
	diagnostic := []byte{4, 5, 6, '\n'}
	if _, err := stdout.Write(out); err != nil {
		t.Fatal(err)
	}
	if _, err := stderr.Write(diagnostic); err != nil {
		t.Fatal(err)
	}
	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatal(err)
	}
	var gotOut, gotErr bytes.Buffer
	if err := output.replay(&gotOut, &gotErr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOut.Bytes(), out) || !bytes.Equal(gotErr.Bytes(), diagnostic) {
		t.Fatalf("elevated output was lost or mixed: stdout=%v stderr=%v", gotOut.Bytes(), gotErr.Bytes())
	}
}

func TestElevatedServiceParamsPreserveInvokingUserRoot(t *testing.T) {
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"kent.exe", "service", "install", "--force", "--no-start"}
	root := t.TempDir()
	t.Setenv(brand.PersistenceRootEnvName, root)
	want := append(slices.Clone(os.Args[1:]), "--persistence-root", root)
	got, err := elevatedServiceParams()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("elevated arguments = %v; want %v", got, want)
	}
}

func TestElevatedServiceOutputRejectsMissingFiles(t *testing.T) {
	_, stdout, stderr, err := openElevatedServiceOutput([]string{
		elevatedServiceOutputFlag, t.TempDir() + `\absent`, t.TempDir() + `\absent`, "service", "install",
	})
	if err == nil || stdout != nil || stderr != nil {
		t.Fatal("missing output files must fail instead of losing diagnostics")
	}
}

func TestElevatedServiceOutputRequiresBothStreams(t *testing.T) {
	output, err := captureElevatedServiceOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := output.close(); err != nil {
			t.Error(err)
		}
	})
	args := output.commandArgs([]string{"service", "install"})
	args[2] = t.TempDir() + `\absent`
	_, stdout, stderr, err := openElevatedServiceOutput(args)
	if !errors.Is(err, os.ErrNotExist) || stdout != nil || stderr != nil {
		t.Fatal("a missing stderr file must not leave a partially redirected invocation")
	}
}

func TestElevatedServiceOutputRejectsInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{
		{elevatedServiceOutputFlag},
		{elevatedServiceOutputFlag, "stdout", "stderr", "serve"},
		{elevatedServiceOutputFlag, "stdout", "stderr", "service", "run"},
	} {
		if _, stdout, stderr, err := openElevatedServiceOutput(args); err == nil || stdout != nil || stderr != nil {
			t.Fatalf("invalid invocation must fail before redirecting: %v", args)
		}
	}
}

func TestOrdinaryServiceInvocationDoesNotRedirectOutput(t *testing.T) {
	want := []string{"service", "status"}
	got, stdout, stderr, err := openElevatedServiceOutput(want)
	if err != nil || stdout != nil || stderr != nil || !slices.Equal(got, want) {
		t.Fatalf("ordinary arguments unexpectedly redirected: %v %v", got, err)
	}
}
