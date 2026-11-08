//go:build !unix

package fsutil

import "os"

// On non-unix platforms locking is best-effort: a lock always succeeds.
// Strata's supported platforms (darwin, linux) use flock.
func flock(*os.File, bool) error { return nil }

func funlock(*os.File) error { return nil }
