package wasi

// WASI snapshot-preview1 __wasi_rights_t bits and precomputed masks for
// internal/wasi (stdio seeding). Bit positions match wasihost_const.go.
const (
	rightFDRead           uint64 = 1 << 1  // __WASI_RIGHT_FD_READ
	rightFDFdstatSetFlags uint64 = 1 << 3  // __WASI_RIGHT_FD_FDSTAT_SET_FLAGS
	rightFDWrite          uint64 = 1 << 6  // __WASI_RIGHT_FD_WRITE
	rightFDFilestatGet    uint64 = 1 << 21 // __WASI_RIGHT_FD_FILESTAT_GET

	// rightsCharDev is the rights_base mask for char-device stdio (stdin/stdout/stderr).
	rightsCharDev = rightFDRead | rightFDWrite | rightFDFdstatSetFlags | rightFDFilestatGet
)
