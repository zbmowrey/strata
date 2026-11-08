package fsutil

import (
	"bytes"
	"strings"
)

// Markers delimit a managed block. Begin and End are matched as line
// prefixes, so a begin line may carry a version or comment after the prefix.
type Markers struct {
	Begin string // full begin line written when the block is created
	End   string // full end line
	// BeginPrefix/EndPrefix match existing lines; default to Begin/End.
	BeginPrefix, EndPrefix string
}

func (m Markers) prefixes() (begin, end string) {
	begin, end = m.BeginPrefix, m.EndPrefix
	if begin == "" {
		begin = m.Begin
	}
	if end == "" {
		end = m.End
	}
	return begin, end
}

// FindBlock locates a managed block. It returns the byte offsets of the start
// of the begin line and the end of the end line (including its newline), and
// whether both markers were found in order.
func FindBlock(content []byte, m Markers) (start, end int, ok bool) {
	bp, ep := m.prefixes()
	start = -1
	for pos := 0; pos < len(content); {
		nl := bytes.IndexByte(content[pos:], '\n')
		next := len(content)
		if nl >= 0 {
			next = pos + nl + 1
		}
		line := strings.TrimRight(string(content[pos:next]), "\r\n")
		switch {
		case start < 0 && strings.HasPrefix(line, bp):
			start = pos
		case start >= 0 && strings.HasPrefix(line, ep):
			return start, next, true
		}
		pos = next
	}
	return 0, 0, false
}

// BlockBody returns the content between the markers (excluding them).
func BlockBody(content []byte, m Markers) ([]byte, bool) {
	start, end, ok := FindBlock(content, m)
	if !ok {
		return nil, false
	}
	block := content[start:end]
	first := bytes.IndexByte(block, '\n')
	lastStart := bytes.LastIndexByte(bytes.TrimRight(block, "\n"), '\n')
	if first < 0 || lastStart <= first {
		return []byte{}, true
	}
	return block[first+1 : lastStart+1], true
}

// ReplaceBlock replaces the managed block's body (and refreshes its marker
// lines). If the block is absent it is appended, separated by a blank line.
// It reports whether the content changed. Content outside the markers is
// never modified.
func ReplaceBlock(content []byte, m Markers, body string) ([]byte, bool) {
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	block := m.Begin + "\n" + body + m.End + "\n"
	start, end, ok := FindBlock(content, m)
	var out []byte
	if ok {
		out = make([]byte, 0, len(content)-end+start+len(block))
		out = append(out, content[:start]...)
		out = append(out, block...)
		out = append(out, content[end:]...)
	} else {
		out = append(out, content...)
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, '\n')
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n\n")) {
			out = append(out, '\n')
		}
		out = append(out, block...)
	}
	return out, !bytes.Equal(out, content)
}

// RemoveBlock deletes the managed block (and one following blank line).
func RemoveBlock(content []byte, m Markers) ([]byte, bool) {
	start, end, ok := FindBlock(content, m)
	if !ok {
		return content, false
	}
	if bytes.HasPrefix(content[end:], []byte("\n")) {
		end++
	}
	out := append(append([]byte{}, content[:start]...), content[end:]...)
	return out, true
}
