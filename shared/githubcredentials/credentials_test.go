package githubcredentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentToken(t *testing.T) {
	for _, test := range []struct {
		values map[string]string
		want   *string
	}{
		{map[string]string{}, nil},
		{map[string]string{"GITHUB_TOKEN": "github"}, pointer("github")},
		{map[string]string{"GH_TOKEN": "gh", "GITHUB_TOKEN": "github"}, pointer("gh")},
	} {
		actual := EnvironmentToken(func(name string) (string, bool) { value, ok := test.values[name]; return value, ok })
		if (actual == nil) != (test.want == nil) || actual != nil && *actual != *test.want {
			t.Fatalf("got %v want %v", actual, test.want)
		}
	}
}

func pointer(value string) *string { return &value }

func TestLoginDiscovery(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	token, err := LoginToken(t.Context())
	if err != nil || token != nil {
		t.Fatalf("missing gh: %v %v", token, err)
	}
	executable := filepath.Join(directory, "gh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$1 $2 $3 $4 $5\" = 'auth token --hostname github.com ' ] || exit 2\necho fake-login\n"), 0700); err != nil {
		t.Fatal(err)
	}
	token, err = LoginToken(t.Context())
	if err != nil || token == nil || *token != "fake-login" {
		t.Fatalf("lookup: %v %v", token, err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho secret-stdout\necho discovery-failed >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	token, err = LoginToken(t.Context())
	if token != nil || err == nil {
		t.Fatalf("failed discovery: %v %v", token, err)
	}
}
