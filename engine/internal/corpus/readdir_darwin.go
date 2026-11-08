//go:build darwin

package corpus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// attrList mirrors struct attrlist (sys/attr.h).
type attrList struct {
	bitmapCount uint16
	reserved    uint16
	commonAttr  uint32
	volAttr     uint32
	dirAttr     uint32
	fileAttr    uint32
	forkAttr    uint32
}

const (
	attrCmnReturnedAttrs = 0x80000000
	attrCmnName          = 0x00000001
	attrCmnObjType       = 0x00000008
	attrCmnModTime       = 0x00000400
	attrFileDataLength   = 0x00000200
	vReg                 = 1
	vDir                 = 2
)

// readDir uses getattrlistbulk(2): one syscall returns names, types, sizes
// and mtimes for many entries, avoiding a per-file lstat (APFS serializes
// lstat, which made a 10k-file walk ~3× slower). Falls back to the portable
// implementation on any error (e.g. filesystems without bulk support).
func readDirFile(f *os.File) ([]entry, error) {
	out, err := readDirBulk(f)
	if err != nil {
		return readDirPortable(f)
	}
	return out, nil
}

// openChild opens a subdirectory relative to its parent's descriptor, which
// avoids re-resolving the full path for every directory.
func openChild(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0) //nolint:gosec // G115: fds fit in int
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: parent.Name() + "/" + name, Err: err}
	}
	return os.NewFile(uintptr(fd), parent.Name()+"/"+name), nil
}

func readDirBulk(f *os.File) ([]entry, error) {
	al := attrList{bitmapCount: 5, commonAttr: attrCmnReturnedAttrs | attrCmnName | attrCmnObjType | attrCmnModTime, fileAttr: attrFileDataLength}
	bp := bufPool.Get().(*[]byte) //nolint:forcetypeassert // the pool only holds *[]byte
	defer bufPool.Put(bp)
	buf := *bp
	var out []entry
	for {
		// x/sys has no libSystem wrapper for getattrlistbulk; the raw syscall
		// is stable ABI and any failure falls back to the portable reader.
		n, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTBULK, f.Fd(), uintptr(unsafe.Pointer(&al)), //nolint:gosec,staticcheck // G103/SA1019: no libSystem wrapper exists; see comment
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0) //nolint:gosec // G103: syscall ABI requires raw pointers
		if errno != 0 {
			return nil, fmt.Errorf("getattrlistbulk: %w", errno)
		}
		if n == 0 {
			return out, nil
		}
		parsed, err := parseBulk(buf, int(n))
		if err != nil {
			return nil, err
		}
		out = append(out, parsed...)
	}
}

var bufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}

var errBulk = errors.New("malformed getattrlistbulk record")

// parseBulk decodes n packed records: u32 length, attribute_set_t (5×u32),
// attrreference_t name, u32 objtype, timespec mtime, [off_t datalength].
func parseBulk(buf []byte, n int) ([]entry, error) {
	le := binary.LittleEndian
	out := make([]entry, 0, n)
	off := 0
	for range n {
		if off+4 > len(buf) {
			return nil, errBulk
		}
		length := int(le.Uint32(buf[off:]))
		rec := buf[off : off+length]
		if length < 52 {
			return nil, errBulk
		}
		returnedFile := le.Uint32(rec[4+12:])
		p := 24
		nameOff := int(int32(le.Uint32(rec[p:]))) //nolint:gosec // G115: attrreference offsets are signed 32-bit
		nameLen := int(le.Uint32(rec[p+4:]))
		start := p + nameOff
		if start < 0 || nameLen < 1 || start+nameLen > len(rec) {
			return nil, errBulk
		}
		e := entry{name: string(rec[start : start+nameLen-1])}
		p += 8
		objType := le.Uint32(rec[p:])
		p += 4
		e.modNS = int64(le.Uint64(rec[p:]))*1e9 + int64(le.Uint64(rec[p+8:])) //nolint:gosec // G115: timespec fields are signed
		p += 16
		e.isDir, e.regular = objType == vDir, objType == vReg
		if returnedFile&attrFileDataLength != 0 && p+8 <= len(rec) {
			e.size = int64(le.Uint64(rec[p:])) //nolint:gosec // G115: off_t is signed
		}
		out = append(out, e)
		off += length
	}
	return out, nil
}
