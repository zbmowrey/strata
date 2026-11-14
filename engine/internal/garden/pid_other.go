//go:build !unix

package garden

// pidAlive cannot probe processes here; the lease ttl decides liveness.
func pidAlive(int) bool { return true }
