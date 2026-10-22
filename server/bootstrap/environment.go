package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

// LoadEnvironment creates the server persistence-root file if missing and reads
// its values. It never changes the process environment, so dotenv values cannot
// be inherited by agent shells.
func LoadEnvironment(root string) (func(string) (string, bool), error) {
	path := filepath.Join(root, ".env")
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		file, err = os.Open(path)
	}
	if err != nil {
		return nil, fmt.Errorf("open server environment %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat server environment %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("server environment %s must be a regular file", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("server environment %s must be an owner-only regular file (chmod 600)", path)
	}
	values, err := godotenv.Parse(file)
	if err != nil {
		// Parser errors can include the secret-bearing input line.
		return nil, fmt.Errorf("cannot parse server environment %s; check dotenv syntax and readability", path)
	}
	return func(key string) (string, bool) {
		if value, present := os.LookupEnv(key); present {
			return value, true
		}
		value, present := values[key]
		return value, present
	}, nil
}
