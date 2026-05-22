package wasihost

import "testing"

// Bit tables duplicated from wasi-testsuite/tests/rust/wasm32-wasip1/src/bin/path_open_preopen.rs
// (wasi crate 0.11.0 snapshot-preview1). Rights use wasihost.go __wasi_rights_t constants.

type wasiTestsuiteRight struct {
	name  string
	right uint64
}

func wasiTestsuiteDirectoryBaseRights() []wasiTestsuiteRight {
	return []wasiTestsuiteRight{
		{"PATH_CREATE_DIRECTORY", rightPathCreateDirectory},
		{"PATH_CREATE_FILE", rightPathCreateFile},
		{"PATH_LINK_SOURCE", rightPathLinkSource},
		{"PATH_LINK_TARGET", rightPathLinkTarget},
		{"PATH_OPEN", rightPathOpen},
		{"FD_READDIR", rightFDReaddir},
		{"PATH_READLINK", rightPathReadlink},
		{"PATH_RENAME_SOURCE", rightPathRenameSource},
		{"PATH_RENAME_TARGET", rightPathRenameTarget},
		{"PATH_SYMLINK", rightPathSymlink},
		{"PATH_REMOVE_DIRECTORY", rightPathRemoveDirectory},
		{"PATH_UNLINK_FILE", rightPathUnlinkFile},
		{"PATH_FILESTAT_GET", rightPathFilestatGet},
		{"PATH_FILESTAT_SET_SIZE", rightPathFilestatSetSize},
		{"PATH_FILESTAT_SET_TIMES", rightPathFilestatSetTimes},
		{"FD_FILESTAT_GET", rightFDFilestatGet},
		{"FD_FILESTAT_SET_TIMES", rightFDFilestatSetTimes},
	}
}

func wasiTestsuiteDirectoryInheritingRights() []wasiTestsuiteRight {
	base := wasiTestsuiteDirectoryBaseRights()
	rights := make([]wasiTestsuiteRight, 0, len(base)+13)
	for _, r := range base {
		if r.right == rightPathFilestatSetSize {
			continue
		}
		rights = append(rights, r)
	}
	rights = append(rights,
		wasiTestsuiteRight{"FD_DATASYNC", rightFDDatasync},
		wasiTestsuiteRight{"FD_READ", rightFDRead},
		wasiTestsuiteRight{"FD_SEEK", rightFDSeek},
		wasiTestsuiteRight{"FD_FDSTAT_SET_FLAGS", rightFDFdstatSetFlags},
		wasiTestsuiteRight{"FD_SYNC", rightFDSync},
		wasiTestsuiteRight{"FD_TELL", rightFDTell},
		wasiTestsuiteRight{"FD_WRITE", rightFDWrite},
		wasiTestsuiteRight{"FD_ADVISE", rightFDAdvise},
		wasiTestsuiteRight{"FD_ALLOCATE", rightFDAllocate},
		wasiTestsuiteRight{"FD_FILESTAT_GET", rightFDFilestatGet},
		wasiTestsuiteRight{"FD_FILESTAT_SET_SIZE", rightFDFilestatSetSize},
		wasiTestsuiteRight{"FD_FILESTAT_SET_TIMES", rightFDFilestatSetTimes},
		wasiTestsuiteRight{"POLL_FD_READWRITE", rightPollFDReadwrite},
	)
	return rights
}

func wasiTestsuiteDirectoryBaseRightsMask() uint64 {
	var mask uint64
	for _, r := range wasiTestsuiteDirectoryBaseRights() {
		mask |= r.right
	}
	return mask
}

func wasiTestsuiteDirectoryInheritingRightsMask() uint64 {
	var mask uint64
	for _, r := range wasiTestsuiteDirectoryInheritingRights() {
		mask |= r.right
	}
	return mask
}

// TestWritableDirPreopenRightsMatchWasiTestsuiteMasks ties production preopen masks to path_open_preopen.rs.
func TestWritableDirPreopenRightsMatchWasiTestsuiteMasks(t *testing.T) {
	t.Parallel()
	if rightsWritableDirPreopenBase != wasiTestsuiteDirectoryBaseRightsMask() {
		t.Errorf("rightsWritableDirPreopenBase = %#x, want wasi-testsuite base %#x",
			rightsWritableDirPreopenBase, wasiTestsuiteDirectoryBaseRightsMask())
	}
	if rightsWritableDirPreopenInheriting != wasiTestsuiteDirectoryInheritingRightsMask() {
		t.Errorf("rightsWritableDirPreopenInheriting = %#x, want wasi-testsuite inheriting %#x",
			rightsWritableDirPreopenInheriting, wasiTestsuiteDirectoryInheritingRightsMask())
	}
}
