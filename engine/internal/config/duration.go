package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a human duration such as "180d", "2w", "12h", "30m" or "2s".
// Zero means "never" where the field allows it.
type Duration struct {
	Seconds int64
}

// Days returns a Duration of n days.
func Days(n int) Duration { return Duration{Seconds: int64(n) * 86400} }

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d.Seconds) * time.Second }

// DaysCount returns the number of whole days.
func (d Duration) DaysCount() int { return int(d.Seconds / 86400) }

var units = []struct {
	suffix string
	secs   int64
}{{"w", 7 * 86400}, {"d", 86400}, {"h", 3600}, {"m", 60}, {"s", 1}}

// ParseDuration parses "<n><unit>" with unit w|d|h|m|s, or "never"/"0".
func ParseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	if s == "never" || s == "0" {
		return Duration{}, nil
	}
	for _, u := range units {
		num, ok := strings.CutSuffix(s, u.suffix)
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(num, 10, 64)
		if err != nil || n < 0 || n > 1<<31 {
			return Duration{}, fmt.Errorf("invalid duration %q", s)
		}
		return Duration{Seconds: n * u.secs}, nil
	}
	return Duration{}, fmt.Errorf("invalid duration %q (want e.g. 180d, 2w, 12h, 30m, 5s)", s)
}

// String formats the duration in the largest exact unit.
func (d Duration) String() string {
	if d.Seconds == 0 {
		return "0"
	}
	for _, u := range units {
		if d.Seconds%u.secs == 0 {
			return strconv.FormatInt(d.Seconds/u.secs, 10) + u.suffix
		}
	}
	return strconv.FormatInt(d.Seconds, 10) + "s"
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }
