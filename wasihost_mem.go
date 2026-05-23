package wasihost

import "encoding/binary"

// readBytes reads length bytes from guest memory starting at ptr.
// Returns nil if ptr or length is zero.
func (s *State) readBytes(ptr, length int32) []byte {
	if ptr == 0 || length == 0 {
		return nil
	}
	return s.mem()[ptr : ptr+length]
}

// pathHasTrailingSlash reports whether the guest path at (ptr, len) ends
// with '/'. This checks the raw guest bytes before any host resolution so
// that trailing-slash semantics are preserved exactly as the guest expressed
// them.

// pathHasTrailingSlash reports whether the guest path at (ptr, len) ends
// with '/'. This checks the raw guest bytes before any host resolution so
// that trailing-slash semantics are preserved exactly as the guest expressed
// them.
func (s *State) pathHasTrailingSlash(ptr, length int32) bool {
	b := s.readBytes(ptr, length)
	return len(b) > 0 && b[len(b)-1] == '/'
}

// openDevNull opens a /dev/null character device and returns its fd.

// writeStringTableSizes writes the item count and total buffer size
// (sum of len(item)+1 for each item) to countPtr and bufSizePtr in mem.
// Shared by environ_sizes_get and args_sizes_get.
func writeStringTableSizes(mem []byte, countPtr, bufSizePtr int32, items []string) {
	binary.LittleEndian.PutUint32(mem[countPtr:], uint32(len(items)))
	var total uint32
	for _, s := range items {
		total += uint32(len(s)) + 1
	}
	binary.LittleEndian.PutUint32(mem[bufSizePtr:], total)
}

// writeStringTable writes a pointer array at ptrBase and null-terminated
// strings packed at bufBase into mem. Each pointer is a uint32 offset into
// mem pointing to the start of the corresponding string. Shared by
// environ_get and args_get.

// writeStringTable writes a pointer array at ptrBase and null-terminated
// strings packed at bufBase into mem. Each pointer is a uint32 offset into
// mem pointing to the start of the corresponding string. Shared by
// environ_get and args_get.
func writeStringTable(mem []byte, ptrBase, bufBase int32, items []string) {
	bufOff := uint32(bufBase)
	for i, s := range items {
		binary.LittleEndian.PutUint32(mem[ptrBase+int32(i*4):], bufOff)
		n := copy(mem[bufOff:], s)
		mem[bufOff+uint32(n)] = 0
		bufOff += uint32(n) + 1
	}
}

// Xfd_pread implements fd_pread. Reads from fd at the given offset
// without updating the fd's WASI-level offset (entry.offset). Requires
// the underlying file to implement ReadAt; if it does not, no bytes are
// read and ENOTSUP is returned. Returns EINVAL for fds 0-2 (positioned
// reads on stdio are not defined by WASI). Returns EISDIR for directory
// fds, ENOTCAPABLE when FD_READ is not set in the fd's rights_base.
