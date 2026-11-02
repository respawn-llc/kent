package architectureguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInMemorySQLitePolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		file   string
		body   string
		reject bool
	}{
		{"disk", "fixture_test.go", `package fixture
import "database/sql"
func fixture() { sql.Open("sqlite", "file:fixture.db") }`, true},
		{"memory", "fixture_test.go", `package fixture
import "database/sql"
func fixture() { sql.Open("sqlite", ":memory:") }`, false},
		{"named memory", "fixture_test.go", `package fixture
import "database/sql"
func fixture() { sql.Open("sqlite", "file:fixture?mode=memory&cache=shared") }`, false},
		{"formatted DSN", "fixture_test.go", `package fixture
import ("database/sql"; "fmt")
func fixture() { sql.Open("sqlite", fmt.Sprintf("file:fixture-%d?mode=memory&cache=shared", 1)) }`, true},
		{"alias", "fixture_test.go", `package fixture
import database "core/server/metadata"
func fixture() { database.Open("root") }`, true},
		{"factory value", "fixture_test.go", `package fixture
import database "core/server/metadata"
func fixture() { open := database.Open; open("root") }`, true},
		{"SQL factory value", "fixture_test.go", `package fixture
import "database/sql"
func fixture() { open := sql.Open; open("sqlite", "fixture.db") }`, true},
		{"named memory with timeout", "fixture_test.go", `package fixture
import ("database/sql"; "fmt")
func fixture() { sql.Open("sqlite", fmt.Sprintf("file:fixture-%d?mode=memory&_pragma=busy_timeout(%d)", 1, 5000)) }`, true},
		{"structured memory with timeout", "fixture_test.go", `package fixture
import ("database/sql"; "net/url"; "strconv")
func fixture() { sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: "fixture", RawQuery: url.Values{"mode": {"memory"}, "_pragma": {"busy_timeout("+strconv.Itoa(5000)+")"}}.Encode()}).String()) }`, false},
		{"structured disk", "fixture_test.go", `package fixture
import ("database/sql"; "net/url")
func fixture() { sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: "fixture", RawQuery: url.Values{"mode": {"rwc"}}.Encode()}).String()) }`, true},
		{"dot SQL", "fixture_test.go", `package fixture
import . "database/sql"
func fixture() { Open("sqlite", "fixture.db") }`, true},
		{"connector", "fixture_test.go", `package fixture
import "database/sql"
func fixture() { sql.OpenDB(connector) }`, true},
		{"production process startup", "fixture_test.go", `package fixture
import stub "core/internal/testharness/pty/blackbox"
func fixture() { stub.NewIsolatedEnvironment("binary", nil) }`, true},
		{"same package", "server/metadata/fixture_test.go", `package metadata
func fixture() { OpenAtPath("root", "database") }`, true},
		{"same file declaration", "server/metadata/fixture_test.go", `package metadata
func OpenAtPath(root, database string) {}
func fixture() { OpenAtPath("root", "database") }`, true},
		{"local declaration", "server/metadata/fixture_test.go", `package metadata
func fixture() { OpenAtPath := func(string, string) {}; OpenAtPath("root", "database") }`, false},
		{"unrelated package name", "other/fixture_test.go", `package metadata
func OpenAtPath(root, database string) {}
func fixture() { OpenAtPath("root", "database") }`, false},
		{"same package qualified other owner", "server/core/fixture_test.go", `package core
import "errors"
func fixture() { errors.New("failure") }`, false},
		{"same package qualified SQL", "server/metadata/fixture_test.go", `package metadata
import "database/sql"
func fixture() { sql.Open("sqlite", ":memory:") }`, false},
		{"dot import", "fixture_test.go", `package fixture
import . "core/server/metadata"
func fixture() { Open("root") }`, true},
		{"shadowed import", "fixture_test.go", `package fixture
import "core/server/metadata"
func fixture() { metadata := object{}; metadata.Open() }
type object struct{}
func (object) Open() {}`, false},
		{"support", "internal/testharness/fixture.go", `package fixture
import "core/server/core"
func fixture() { core.New(nil, nil, nil) }`, true},
		{"implicit options", "fixture_test.go", `package fixture
import "core/server/core"
func fixture() { core.NewWithContextOptions(nil, nil, nil, nil, core.Options{}) }`, true},
		{"explicit options", "fixture_test.go", `package fixture
import "core/server/core"
func fixture() { core.NewWithContextOptions(nil, nil, nil, nil, core.Options{MetadataStore: store}) }`, false},
		{"assigned options", "fixture_test.go", `package fixture
import "core/server/core"
func fixture(options core.Options) { options.MetadataStore = store; core.NewWithContextOptions(nil, nil, nil, nil, options) }`, true},
		{"reset options", "fixture_test.go", `package fixture
import "core/server/core"
func fixture(options core.Options) { options.MetadataStore = store; options.MetadataStore = nil; core.NewWithContextOptions(nil, nil, nil, nil, options) }`, true},
		{"production", "fixture.go", `package fixture
import "core/server/metadata"
func fixture() { metadata.Open("root") }`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, test.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			writeGuardFixture(t, root, test.file, test.body)
			err := CheckInMemoryTestSQLite(root)
			if (err != nil) != test.reject {
				t.Fatalf("policy error = %v, reject = %v", err, test.reject)
			}
		})
	}
}

func TestInMemorySQLitePolicyCrossFileOwner(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "server", "metadata")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGuardFixture(t, directory, "store.go", `package metadata
func Open(root string) {}`)
	writeGuardFixture(t, directory, "store_test.go", `package metadata
func fixture() { Open("root") }`)
	if err := CheckInMemoryTestSQLite(root); err == nil {
		t.Fatal("expected a cross-file disk constructor violation")
	}
}
