package fsutil

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "sub", "x.lock")
	l1, err := TryLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	if _, err := LockWait(p, 20*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("wait lock: %v", err)
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = l1.Unlock()
	}()
	l2, err := LockWait(p, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := l2.Unlock(); err != nil {
		t.Fatal("double unlock")
	}
	var nilLock *Lock
	if nilLock.Unlock() != nil {
		t.Fatal("nil unlock")
	}
	if _, err := TryLock(filepath.Join(p, "under-a-file", "x")); err == nil {
		t.Fatal("lock under a file should fail")
	}
}

var md = Markers{Begin: "<!-- strata:begin v1 -->", End: "<!-- strata:end -->", BeginPrefix: "<!-- strata:begin"}

func TestReplaceBlock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, body, want string
		changed              bool
	}{
		{"append to empty", "", "hello", "<!-- strata:begin v1 -->\nhello\n<!-- strata:end -->\n", true},
		{"append no newline", "# Title", "x\n", "# Title\n\n<!-- strata:begin v1 -->\nx\n<!-- strata:end -->\n", true},
		{"append after blank", "a\n\n", "x", "a\n\n<!-- strata:begin v1 -->\nx\n<!-- strata:end -->\n", true},
		{"replace keeps outside", "top\n<!-- strata:begin v0 old -->\nold\n<!-- strata:end -->\nbottom\n", "new", "top\n<!-- strata:begin v1 -->\nnew\n<!-- strata:end -->\nbottom\n", true},
		{"idempotent", "a\n<!-- strata:begin v1 -->\nx\n<!-- strata:end -->\n", "x", "a\n<!-- strata:begin v1 -->\nx\n<!-- strata:end -->\n", false},
		{"crlf end marker", "<!-- strata:begin v1 -->\r\nx\r\n<!-- strata:end -->", "y", "<!-- strata:begin v1 -->\ny\n<!-- strata:end -->\n", true},
		{"empty body", "", "", "<!-- strata:begin v1 -->\n<!-- strata:end -->\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, changed := ReplaceBlock([]byte(tc.in), md, tc.body)
			if string(got) != tc.want || changed != tc.changed {
				t.Fatalf("got %q changed=%v", got, changed)
			}
		})
	}
}

func TestBlockBodyAndRemove(t *testing.T) {
	t.Parallel()
	in := []byte("a\n<!-- strata:begin v1 -->\nl1\nl2\n<!-- strata:end -->\n\nb\n")
	body, ok := BlockBody(in, md)
	if !ok || string(body) != "l1\nl2\n" {
		t.Fatalf("body %q", body)
	}
	if body, ok := BlockBody([]byte("<!-- strata:begin\n<!-- strata:end -->\n"), md); !ok || len(body) != 0 {
		t.Fatalf("empty body %q", body)
	}
	if _, ok := BlockBody([]byte("nothing"), md); ok {
		t.Fatal("found missing block")
	}
	out, ok := RemoveBlock(in, md)
	if !ok || string(out) != "a\nb\n" {
		t.Fatalf("remove %q", out)
	}
	if _, ok := RemoveBlock([]byte("x"), md); ok {
		t.Fatal("removed missing block")
	}
	// An end marker before the begin marker is not a block.
	if _, _, ok := FindBlock([]byte("<!-- strata:end -->\n<!-- strata:begin v1 -->\n"), md); ok {
		t.Fatal("unordered markers matched")
	}
	plain := Markers{Begin: "# >>> x", End: "# <<< x"}
	if out, _ := ReplaceBlock(nil, plain, "y"); string(out) != "# >>> x\ny\n# <<< x\n" {
		t.Fatalf("plain markers %q", out)
	}
}
