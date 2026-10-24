package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gofrs/flock"
)

func main() {
	if err := prepare(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command(args ...string) ([]byte, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", cmd.String(), err, output)
	}
	return output, nil
}

func prepare() (result error) {
	if err := os.MkdirAll(".generated", 0o755); err != nil {
		return err
	}
	lock := flock.New(".generated/openai-sdk.lock", flock.SetPermissions(0o600))
	defer func() { result = errors.Join(result, lock.Close()) }()
	if err := lock.Lock(); err != nil {
		return err
	}
	data, err := command("go", "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var manifest struct {
		Require []struct{ Path, Version string }
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	var version *string
	for _, dependency := range manifest.Require {
		if dependency.Path == "github.com/openai/openai-go/v3" {
			value := dependency.Version
			version = &value
		}
	}
	if version == nil {
		return fmt.Errorf("OpenAI SDK dependency is missing")
	}
	data, err = command("go", "mod", "download", "-json", "github.com/openai/openai-go/v3@"+*version)
	if err != nil {
		return err
	}
	var download struct{ Dir, Sum, GoModSum string }
	if err := json.Unmarshal(data, &download); err != nil {
		return err
	}
	checksums, err := os.ReadFile("patches/openai-go/upstream.sum")
	if err != nil {
		return err
	}
	expected := fmt.Sprintf("github.com/openai/openai-go/v3 %s %s\ngithub.com/openai/openai-go/v3 %s/go.mod %s\n", *version, download.Sum, *version, download.GoModSum)
	if string(checksums) != expected {
		return fmt.Errorf("OpenAI SDK upstream checksum does not match patches/openai-go/upstream.sum")
	}
	patch, err := os.ReadFile("patches/openai-go/bare-error.patch")
	if err != nil {
		return err
	}
	test, err := os.ReadFile("patches/openai-go/bare_error_test.go.txt")
	if err != nil {
		return err
	}
	fingerprint := sha256.New()
	for _, input := range [][]byte{checksums, patch, test} {
		if _, err := fingerprint.Write(input); err != nil {
			return err
		}
	}
	stamp := fmt.Sprintf("%x", fingerprint.Sum(nil))
	target := ".generated/openai-sdk"
	existing, err := os.ReadFile(filepath.Join(target, ".kent-prepared"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && string(existing) == stamp {
		return nil
	}
	staging, err := os.MkdirTemp(".generated", "openai-sdk-")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, os.RemoveAll(staging)) }()
	prepared := filepath.Join(staging, "prepared")
	if err := os.Mkdir(prepared, 0o755); err != nil {
		return err
	}
	if err := os.CopyFS(prepared, os.DirFS(download.Dir)); err != nil {
		return err
	}
	apply := exec.Command("patch", "--batch", "--forward", "--fuzz=0", "-p1")
	apply.Dir = prepared
	apply.Stdin = bytes.NewReader(patch)
	apply.Stdout, apply.Stderr = os.Stdout, os.Stderr
	if err := apply.Run(); err != nil {
		return fmt.Errorf("apply OpenAI SDK decoder patch: %w", err)
	}
	if err := os.WriteFile(filepath.Join(prepared, "responses", "kent_bare_error_test.go"), test, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(prepared, ".kent-prepared"), []byte(stamp), 0o644); err != nil {
		return err
	}
	if err := os.Rename(target, filepath.Join(staging, "previous")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(prepared, target)
}
