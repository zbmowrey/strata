// Package version holds build information and semantic-version comparison.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Set by the linker: -X github.com/zbmowrey/strata/engine/internal/version.Version=...
var (
	Version = "0.1.0-dev"
	Commit  = ""
	Date    = ""
)

// Info is the build information printed by `strata version`.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// Get returns the build information, filling commit/date from the embedded
// VCS stamp when ldflags did not set them.
func Get() Info {
	info := Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if info.Commit != "" {
		return info
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if info.Version == "0.1.0-dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		info.Version = strings.TrimPrefix(bi.Main.Version, "v")
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
		case "vcs.time":
			info.Date = s.Value
		}
	}
	return info
}

// Semver is a parsed MAJOR.MINOR.PATCH[-pre] version.
type Semver struct {
	Pre                 string
	Major, Minor, Patch int
}

// Parse parses a version such as "0.1.0", "v1.2.3" or "0.1.0-dev".
func Parse(s string) (Semver, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Semver{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Semver{}, fmt.Errorf("invalid version %q: bad component %q", s, p)
		}
		n[i] = v
	}
	return Semver{Major: n[0], Minor: n[1], Patch: n[2], Pre: pre}, nil
}

// Compare returns -1, 0 or 1 comparing a and b. A pre-release sorts before
// its release.
func Compare(a, b Semver) int {
	for _, d := range [3]int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d != 0 {
			return sign(d)
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	return strings.Compare(a.Pre, b.Pre)
}

func sign(d int) int {
	if d < 0 {
		return -1
	}
	return 1
}

// AtLeast reports whether have >= want. Unparsable inputs return an error.
func AtLeast(have, want string) (bool, error) {
	h, err := Parse(have)
	if err != nil {
		return false, err
	}
	w, err := Parse(want)
	if err != nil {
		return false, err
	}
	// A dev build of the same release satisfies its own minimum.
	h.Pre, w.Pre = "", ""
	return Compare(h, w) >= 0, nil
}
