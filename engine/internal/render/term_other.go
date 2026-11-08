//go:build !unix

package render

func termWidth(uintptr) int { return 0 }
