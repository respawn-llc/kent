//go:build !unix

package config

func ServerLocalRPCSocketPath(string) (string, bool, error) {
	return "", false, nil
}
