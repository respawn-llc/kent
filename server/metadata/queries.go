package metadata

import (
	"strings"

	_ "embed"
)

//go:embed queries/set_project_key.sql
var setProjectKeyQuery string

func metadataQuery(query string) string {
	return strings.TrimSuffix(query, "\n")
}
