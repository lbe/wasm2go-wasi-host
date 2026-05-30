package wasi

import (
	"reflect"
	"testing"
)

// TestFileEntryWASIPreview1Fields verifies FileEntry carries WASI Preview1 fd
// offset and rights on the entry (not on sys.File), and that InitFSContext
// seeds those fields on stdio entries.
func TestFileEntryWASIPreview1Fields(t *testing.T) {
	typ := reflect.TypeOf(FileEntry{})
	for name, kind := range map[string]reflect.Kind{
		"Offset":           reflect.Int64,
		"RightsBase":       reflect.Uint64,
		"RightsInheriting": reflect.Uint64,
	} {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("FileEntry missing exported field %q", name)
		}
		if f.Type.Kind() != kind {
			t.Fatalf("FileEntry.%s type = %v, want %v", name, f.Type, kind)
		}
	}

	fsc, err := InitFSContext(nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InitFSContext: %v", err)
	}
	defer fsc.Close()

	for _, fd := range []int32{FdStdin, FdStdout, FdStderr} {
		fe, ok := fsc.LookupFile(fd)
		if !ok {
			t.Fatalf("LookupFile(%d): stdio entry not found", fd)
		}
		if fe.RightsBase != rightsCharDev {
			t.Errorf("LookupFile(%d): RightsBase = %#x, want %#x (rightsCharDev)", fd, fe.RightsBase, rightsCharDev)
		}
	}
}
