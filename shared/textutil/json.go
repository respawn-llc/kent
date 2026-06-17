package textutil

import (
	"bytes"
	"encoding/json"
)

// CompactNoHTMLEscape compacts JSON while preserving object key order and
// normalizing Go's HTML escape sequences back to their literal bytes. It leaves
// user-authored escaped backslash-u text intact.
func CompactNoHTMLEscape(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return string(trimmed)
	}
	return string(replaceJSONHTMLEscapes(compact.Bytes()))
}

func replaceJSONHTMLEscapes(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for idx := 0; idx < len(in); {
		if in[idx] != '\\' {
			out = append(out, in[idx])
			idx++
			continue
		}
		if idx+1 < len(in) && in[idx+1] == '\\' {
			out = append(out, in[idx], in[idx+1])
			idx += 2
			continue
		}
		if idx+5 < len(in) && in[idx+1] == 'u' {
			switch string(in[idx+2 : idx+6]) {
			case "0026":
				out = append(out, '&')
				idx += 6
				continue
			case "003c", "003C":
				out = append(out, '<')
				idx += 6
				continue
			case "003e", "003E":
				out = append(out, '>')
				idx += 6
				continue
			}
		}
		out = append(out, in[idx])
		idx++
	}
	return out
}
