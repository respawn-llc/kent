package patchformat

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"

	"core/shared/textutil"
)

func Parse(src string) (Document, error) {
	lines, err := patchBodyLines(src)
	if err != nil {
		return Document{}, err
	}
	s := scanner{lines: lines}
	if !s.consumeMarker("*** Begin Patch") {
		return Document{}, errors.New("patch must start with *** Begin Patch")
	}

	doc := Document{}
	for !s.done() {
		line := strings.TrimSpace(s.peek())
		switch {
		case line == "*** End Patch":
			s.next()
			if !s.done() {
				return doc, errors.New("unexpected content after *** End Patch")
			}
			return doc, nil
		case strings.HasPrefix(line, "*** Add File: "):
			head := strings.TrimPrefix(strings.TrimSpace(s.next()), "*** Add File: ")
			content := []string{}
			for !s.done() {
				n := s.peek()
				if strings.HasPrefix(n, "*** ") {
					break
				}
				if !strings.HasPrefix(n, "+") {
					return Document{}, fmt.Errorf("add file line must start with +: %q", n)
				}
				content = append(content, strings.TrimPrefix(s.next(), "+"))
			}
			if len(content) == 0 {
				return Document{}, fmt.Errorf("add file hunk for path %q is empty", head)
			}
			doc.Hunks = append(doc.Hunks, AddFile{Path: head, Content: content})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimPrefix(strings.TrimSpace(s.next()), "*** Delete File: ")
			doc.Hunks = append(doc.Hunks, DeleteFile{Path: path})
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimPrefix(strings.TrimSpace(s.next()), "*** Update File: ")
			up := UpdateFile{Path: path}
			if !s.done() && strings.HasPrefix(strings.TrimSpace(s.peek()), "*** Move to: ") {
				up.MoveTo = strings.TrimPrefix(strings.TrimSpace(s.next()), "*** Move to: ")
			}
			for !s.done() {
				n := s.peek()
				if strings.TrimSpace(n) == "*** End of File" {
					up.Changes = append(up.Changes, ChangeLine{EndOfFile: true})
					s.next()
					continue
				}
				if strings.HasPrefix(n, "*** ") {
					break
				}
				if n == "" {
					up.Changes = append(up.Changes, ChangeLine{Kind: ' ', Content: ""})
					s.next()
					continue
				}
				p := n[0]
				if p != ' ' && p != '+' && p != '-' && p != '@' {
					return Document{}, fmt.Errorf("invalid update line prefix in %q", n)
				}
				up.Changes = append(up.Changes, ChangeLine{Kind: rune(p), Content: n[1:]})
				s.next()
			}
			if len(up.Changes) == 0 && strings.TrimSpace(up.MoveTo) == "" {
				return Document{}, fmt.Errorf("update file hunk for path %q is empty", path)
			}
			doc.Hunks = append(doc.Hunks, up)
		default:
			return Document{}, fmt.Errorf("unknown patch block: %q", line)
		}
	}

	return Document{}, errors.New("missing *** End Patch")
}

func patchBodyLines(src string) ([]string, error) {
	lines := splitRawLines(strings.TrimSpace(src))
	if len(lines) >= 3 {
		first := lines[0]
		last := lines[len(lines)-1]
		if (first == "<<EOF" || first == "<<'EOF'" || first == "<<\"EOF\"") && last == "EOF" {
			return lines[1 : len(lines)-1], nil
		}
	}
	return lines, nil
}

type scanner struct {
	lines []string
	idx   int
}

func (s *scanner) done() bool {
	return s.idx >= len(s.lines)
}

func (s *scanner) peek() string {
	if s.done() {
		return ""
	}
	return s.lines[s.idx]
}

func (s *scanner) next() string {
	v := s.peek()
	s.idx++
	return v
}

func (s *scanner) consumeMarker(v string) bool {
	if strings.TrimSpace(s.peek()) == v {
		s.next()
		return true
	}
	return false
}

func splitRawLines(in string) []string {
	in = textutil.NormalizeCRLF(in)
	reader := bufio.NewScanner(bytes.NewBufferString(in))
	out := []string{}
	for reader.Scan() {
		out = append(out, reader.Text())
	}
	return out
}
