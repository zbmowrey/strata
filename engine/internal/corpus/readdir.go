package corpus

import (
	"io/fs"
	"os"
)

// entry is one directory entry with the stat fields change detection needs.
type entry struct {
	name    string
	size    int64
	modNS   int64
	isDir   bool
	regular bool
}

// readDirPortable lists an open directory with ReadDir + Lstat per entry.
func readDirPortable(f *os.File) ([]entry, error) {
	des, err := f.ReadDir(-1)
	if err != nil {
		return nil, err //nolint:wrapcheck // wrapped by the walker
	}
	out := make([]entry, 0, len(des))
	for _, d := range des {
		e := entry{name: d.Name(), isDir: d.IsDir(), regular: d.Type().IsRegular()}
		if d.Type()&fs.ModeSymlink != 0 {
			e.isDir, e.regular = false, false
		}
		if e.regular {
			info, err := d.Info()
			if err != nil {
				continue // vanished
			}
			e.size, e.modNS = info.Size(), info.ModTime().UnixNano()
		}
		out = append(out, e)
	}
	return out, nil
}
