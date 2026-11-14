//go:build !unix

package garden

// pidAlive cannot probe processes here; the lease ttl decides liveness.
func pidAlive(int) bool { return true }

// terminate is unsupported here; a hung holder is left to its ttl.
func terminate(int) {}
