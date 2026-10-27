package bootstrap_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"core/server/bootstrap"
)

func TestLoadEnvironmentCreatesPrivateEmptyFile(t *testing.T) {
	root := t.TempDir()
	if _, err := bootstrap.LoadEnvironment(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".env")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("environment file mode = %v, want regular file", info.Mode())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("environment file permissions = %v, want 0600", info.Mode())
	}
	if info.Size() != 0 {
		t.Fatalf("environment file size = %d, want empty", info.Size())
	}
}

func TestLoadEnvironmentPreservesExistingFileAndPrecedence(t *testing.T) {
	t.Setenv("KENT_BOOTSTRAP_TEST_OVERRIDE", "process")
	t.Setenv("KENT_BOOTSTRAP_TEST_EMPTY", "")
	const fileOnly = "KENT_BOOTSTRAP_TEST_FILE_ONLY"
	if _, present := os.LookupEnv(fileOnly); present {
		t.Fatalf("%s must be absent from the test process environment", fileOnly)
	}
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	const contents = "# operator-managed\nKENT_BOOTSTRAP_TEST_FILE_ONLY='file value'\nKENT_BOOTSTRAP_TEST_OVERRIDE=file\nKENT_BOOTSTRAP_TEST_EMPTY=file\n"
	if err := os.WriteFile(path, []byte(contents), 0o400); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		lookup, err := bootstrap.LoadEnvironment(root)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{
			fileOnly:                       "file value",
			"KENT_BOOTSTRAP_TEST_OVERRIDE": "process",
			"KENT_BOOTSTRAP_TEST_EMPTY":    "",
		} {
			if got, present := lookup(key); !present || got != want {
				t.Fatalf("lookup %s = %q, %v; want %q, true", key, got, present, want)
			}
		}
		if _, present := os.LookupEnv(fileOnly); present {
			t.Fatal("file values leaked into the process environment")
		}
		current, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(original, current) || current.Mode() != original.Mode() {
			t.Fatal("existing environment file identity or permissions changed")
		}
		actual, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != contents {
			t.Fatal("existing environment file contents changed")
		}
	}
}

func TestLoadEnvironmentAppliesPlatformPermissionsWithoutChangingExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	const contents = "SECRET=preserve-me\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := bootstrap.LoadEnvironment(root)
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("existing Windows environment file rejected: %v", err)
		}
		if value, present := lookup("SECRET"); !present || value != "preserve-me" {
			t.Fatal("existing Windows environment values not loaded")
		}
	} else if err == nil {
		t.Fatal("unsafe environment file accepted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, info) || info.Mode() != original.Mode() {
		t.Fatal("existing environment file identity or permissions changed")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != contents {
		t.Fatal("unsafe file contents changed")
	}
}

func TestLoadEnvironmentConcurrentCreation(t *testing.T) {
	root := t.TempDir()
	var callers sync.WaitGroup
	for range 16 {
		callers.Go(func() {
			if _, err := bootstrap.LoadEnvironment(root); err != nil {
				t.Error(err)
			}
		})
	}
	callers.Wait()
	info, err := os.Stat(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("concurrent startup left invalid environment file: %v, %d bytes", info.Mode(), info.Size())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("concurrent startup left environment file permissions %v, want 0600", info.Mode())
	}
}

func TestLoadEnvironmentRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.LoadEnvironment(root); err == nil {
		t.Fatal("environment directory accepted")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("environment directory changed")
	}
}
