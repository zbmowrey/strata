package fail

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeAndHint(t *testing.T) {
	t.Parallel()
	base := errors.New("boom")
	tests := []struct {
		err  error
		name string
		hint string
		code int
	}{
		{name: "nil", err: nil, code: OK},
		{name: "plain", err: base, code: Runtime},
		{name: "wrapped", err: Wrap(Usage, "try --help", base), code: Usage, hint: "try --help"},
		{name: "outer wins", err: fmt.Errorf("ctx: %w", Wrap(Partial, "", Wrap(Locked, "inner", base))), code: Partial, hint: "inner"},
		{name: "new", err: New(NotInstance, "run strata init", "no instance at %s", "/x"), code: NotInstance, hint: "run strata init"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CodeOf(tc.err); got != tc.code {
				t.Errorf("CodeOf = %d, want %d", got, tc.code)
			}
			if got := HintOf(tc.err); got != tc.hint {
				t.Errorf("HintOf = %q, want %q", got, tc.hint)
			}
		})
	}
}

func TestErrorBasics(t *testing.T) {
	t.Parallel()
	if Wrap(Usage, "h", nil) != nil {
		t.Fatal("Wrap(nil) must be nil")
	}
	e := New(Usage, "", "bad %d", 3)
	if e.Error() != "bad 3" || e.Unwrap() == nil {
		t.Fatalf("unexpected %v", e)
	}
	if (&Error{}).Error() != "unknown error" {
		t.Fatal("empty error message")
	}
}
