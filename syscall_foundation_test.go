package wasihost

import (
	"encoding/binary"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

func TestFoundationSyscalls(t *testing.T) {
	const (
		countOff   = 100
		bufSizeOff = 108
		envPtrOff  = 200
		envBufOff  = 300
		prestatOff = 400
		statOff    = 500
		pathBufOff = 600
		randOff    = 700
	)

	t.Run("environ_sizes_get writes count and total length", func(t *testing.T) {
		s, buf := newTestState(NewModuleConfig().WithEnvRaw("A=1").WithEnvRaw("BC=2"))
		errno := s.Xenviron_sizes_get(countOff, bufSizeOff)
		if errno != 0 {
			t.Fatalf("expected ESUCCESS, got %d", errno)
		}
		count := binary.LittleEndian.Uint32(buf[countOff : countOff+4])
		size := binary.LittleEndian.Uint32(buf[bufSizeOff : bufSizeOff+4])
		if count != 2 {
			t.Errorf("got count %d, want 2", count)
		}
		if size != 9 {
			t.Errorf("got size %d, want 9", size)
		}
	})

	t.Run("environ_get writes strings and pointers", func(t *testing.T) {
		s, buf := newTestState(NewModuleConfig().WithEnvRaw("A=1").WithEnvRaw("BC=2"))
		errno := s.Xenviron_get(envPtrOff, envBufOff)
		if errno != 0 {
			t.Fatalf("expected ESUCCESS, got %d", errno)
		}
		p1 := binary.LittleEndian.Uint32(buf[envPtrOff : envPtrOff+4])
		p2 := binary.LittleEndian.Uint32(buf[envPtrOff+4 : envPtrOff+8])
		if p1 != uint32(envBufOff) {
			t.Errorf("got p1 %d, want %d", p1, envBufOff)
		}
		if p2 != uint32(envBufOff+4) {
			t.Errorf("got p2 %d, want %d", p2, envBufOff+4)
		}
		s1 := string(buf[envBufOff : envBufOff+4])
		s2 := string(buf[envBufOff+4 : envBufOff+9])
		if s1 != "A=1\x00" {
			t.Errorf("got s1 %q, want %q", s1, "A=1\x00")
		}
		if s2 != "BC=2\x00" {
			t.Errorf("got s2 %q, want %q", s2, "BC=2\x00")
		}
	})

	t.Run("fd_prestat_get and fd_prestat_dir_name", func(t *testing.T) {
		s, buf := newTestState(nil)
		installPreopenAt(t, s, 3, "tmp", t.TempDir(), false)

		t.Run("fd_prestat_get success", func(t *testing.T) {
			errno := s.Xfd_prestat_get(3, prestatOff)
			if errno != 0 {
				t.Fatalf("expected ESUCCESS, got %d", errno)
			}
			prestatType := binary.LittleEndian.Uint32(buf[prestatOff : prestatOff+4])
			pathLen := binary.LittleEndian.Uint32(buf[prestatOff+4 : prestatOff+8])
			if prestatType != 0 {
				t.Errorf("got type %d, want 0", prestatType)
			}
			if pathLen != 3 {
				t.Errorf("got pathLen %d, want 3", pathLen)
			}
		})

		t.Run("fd_prestat_get EBADF", func(t *testing.T) {
			errno := s.Xfd_prestat_get(4, prestatOff)
			if errno != 8 { // EBADF
				t.Errorf("got errno %d, want 8", errno)
			}
		})

		t.Run("fd_prestat_dir_name success", func(t *testing.T) {
			errno := s.Xfd_prestat_dir_name(3, pathBufOff, 3)
			if errno != 0 {
				t.Fatalf("expected ESUCCESS, got %d", errno)
			}
			got := string(buf[pathBufOff : pathBufOff+3])
			if got != "tmp" {
				t.Errorf("got %q, want %q", got, "tmp")
			}
		})
	})

	t.Run("fd_fdstat_get", func(t *testing.T) {
		s, buf := newTestState(nil)
		installPreopenAt(t, s, 3, "tmp", t.TempDir(), false)
		insertStubFD(s, 5, wrapStubFSFile(stubFSFile{}), 0)

		cases := []struct {
			fd   int32
			want uint16
			err  int32
		}{
			{0, 2, 0},
			{1, 2, 0},
			{2, 2, 0},
			{3, 3, 0},
			{5, 4, 0},
			{99, 0, 8},
		}

		for _, tc := range cases {
			errno := s.Xfd_fdstat_get(tc.fd, statOff)
			if errno != tc.err {
				t.Errorf("fd %d: got errno %d, want %d", tc.fd, errno, tc.err)
				continue
			}
			if errno == 0 {
				gotType := binary.LittleEndian.Uint16(buf[statOff : statOff+2])
				if gotType != tc.want {
					t.Errorf("fd %d: got type %d, want %d", tc.fd, gotType, tc.want)
				}
			}
		}
	})

	t.Run("fd_renumber", func(t *testing.T) {
		s, _ := newTestState(nil)
		insertStubFD(s, 5, wrapStubFSFile(stubFSFile{}), 0)
		origRights := lookupFETest(s, 5).RightsBase
		errno := s.Xfd_renumber(5, 6)
		if errno != 0 {
			t.Fatalf("expected ESUCCESS, got %d", errno)
		}
		if _, ok := s.lookupFD(5); ok {
			t.Errorf("fd 5 still open after renumber")
		}
		if fe6, ok := s.lookupFD(6); !ok || fe6.RightsBase != origRights {
			t.Errorf("fd 6 does not match original")
		}
	})

	t.Run("proc_exit panics", func(t *testing.T) {
		s, _ := newTestState(nil)
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic, got nil")
			}
			if exit, ok := r.(ExitError); !ok || exit.Code != 42 {
				t.Errorf("got panic %v, want ExitError{Code: 42}", r)
			}
		}()
		s.Xproc_exit(42)
	})

	t.Run("random_get", func(t *testing.T) {
		s, buf := newTestState(nil)
		errno := s.Xrandom_get(randOff, 32)
		if errno != 0 {
			t.Fatalf("expected ESUCCESS, got %d", errno)
		}
		allZero := true
		for _, b := range buf[randOff : randOff+32] {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			t.Error("random_get filled with all zeros")
		}
	})

	t.Run("resolvePath", func(t *testing.T) {
		s, _ := newTestState(NewModuleConfig().
			WithReadOnlyFS("/", emptyMapFS()).
			WithReadOnlyFS("zeroperl", emptyMapFS()))
		m, rel := s.resolvePath("/zeroperl/lib/x")
		if m == nil {
			t.Fatal("expected a mount, got nil")
		}
		if m.guestPath != "zeroperl" {
			t.Errorf("got mount %q, want %q", m.guestPath, "zeroperl")
		}
		if rel != "lib/x" {
			t.Errorf("got rel %q, want %q", rel, "lib/x")
		}
	})

	t.Run("readBytes ptr==0 returns nil", func(t *testing.T) {
		s, _ := newTestState(nil)
		if got := s.readBytes(0, 10); got != nil {
			t.Errorf("readBytes(0, 10) = %v, want nil", got)
		}
	})

	t.Run("resolveDirfdPath non-preopen dir fd", func(t *testing.T) {
		s, _ := newTestState(NewModuleConfig().WithReadOnlyFS("/tmp", emptyMapFS()))
		installPreopenAt(t, s, 3, "/tmp", t.TempDir(), false)
		subdir := t.TempDir()
		wf, errno := sysfs.DirFS{Path: subdir}.OpenFile(".", wasi.O_RDONLY|wasi.O_DIRECTORY, 0)
		if errno != 0 {
			t.Fatal(errno)
		}
		t.Cleanup(func() { _ = wf.Close() }) // before subdir removal (Windows)
		insertTestFileAt(s, 5, &wasi.FileEntry{
			GuestPath: "/tmp/subdir",
			File:      wf,
		})
		buf := make([]byte, 65536)
		s.mem = func() []byte { return buf }
		copy(buf[1000:], "child.txt")
		mount, rel := s.resolveDirfdPath(5, 1000, 9)
		if mount == nil {
			t.Fatal("expected non-nil mount")
		}
		if rel != "subdir/child.txt" {
			t.Errorf("rel = %q, want subdir/child.txt", rel)
		}
	})
}

func TestWASIStubs(t *testing.T) {
	t.Parallel()

	s := New(nil, nil)

	// No-op stubs returning ESuccess.
	if rc := s.Xfd_fdstat_set_flags(0, 0); rc != wasiESuccess {
		t.Errorf("Xfd_fdstat_set_flags = %d, want ESuccess", rc)
	}
	// Xfd_filestat_set_size requires FD_FILESTAT_SET_SIZE right which stdio lacks.
	if rc := s.Xfd_filestat_set_size(0, 0); rc != wasiENotCap {
		t.Errorf("Xfd_filestat_set_size = %d, want ENOTCAPABLE", rc)
	}
	if rc := s.Xfd_filestat_set_times(0, 0, 0, 0); rc != wasiESuccess {
		t.Errorf("Xfd_filestat_set_times = %d, want ESuccess", rc)
	}
	if rc := s.Xfd_sync(0); rc != wasiESuccess {
		t.Errorf("Xfd_sync = %d, want ESuccess", rc)
	}
	// Xpath_filestat_set_times with fstFlags=0 is a no-op (returns ESUCCESS).
	// With fstMtim flag but no preopen, returns EROFS.
	if rc := s.Xpath_filestat_set_times(0, 0, 0, 0, 0, 0, fstMtim); rc != wasiEROFS {
		t.Errorf("Xpath_filestat_set_times = %d, want EROFS", rc)
	}
	if rc := s.Xcall_host_function(0, 0, 0); rc != 0 {
		t.Errorf("Xcall_host_function = %d, want 0", rc)
	}

	// No-op stubs returning ENoSys (not implemented).
	for name, rc := range map[string]int32{
		"Xproc_raise": s.Xproc_raise(0),
	} {
		if rc != wasiENoSys {
			t.Errorf("%s = %d, want ENoSys", name, rc)
		}
	}

	// Xfd_renumber: invalid fd returns EBadf; valid renumber succeeds.
	if rc := s.Xfd_renumber(-1, 0); rc != wasiEBadf {
		t.Errorf("Xfd_renumber(-1, 0) = %d, want EBadf", rc)
	}
	// Xfd_renumber success: fds 0 and 2 are both valid in the default stdio table.
	if rc := s.Xfd_renumber(0, 2); rc != wasiESuccess {
		t.Errorf("Xfd_renumber(0, 2) = %d, want ESuccess", rc)
	}
	// High unallocated fd_to is valid; FileTable grows via InsertAt.
	if rc := s.Xfd_renumber(1, 100); rc != wasiESuccess {
		t.Errorf("Xfd_renumber(1, 100) = %d, want ESuccess", rc)
	}

	// Path mutation functions: with no mounts configured, all return EROFS.
	for name, rc := range map[string]int32{
		"Xpath_create_directory": s.Xpath_create_directory(0, 0, 0),
		"Xpath_link":             s.Xpath_link(0, 0, 0, 0, 0, 0, 0),
		"Xpath_readlink":         s.Xpath_readlink(0, 0, 0, 0, 0, 0),
		"Xpath_remove_directory": s.Xpath_remove_directory(0, 0, 0),
		"Xpath_symlink":          s.Xpath_symlink(0, 0, 0, 0, 0),
	} {
		if rc != wasiEROFS {
			t.Errorf("%s with no mounts = %d, want EROFS (%d)", name, rc, wasiEROFS)
		}
	}
}

// TestAssertSingleOwner covers the assertOwner invariant paths including the
// panic on cross-goroutine access.
func TestAssertSingleOwner(t *testing.T) {
	t.Parallel()

	t.Run("disabled by default", func(t *testing.T) {
		s := New(nil, nil)
		s.assertSingleOwner()
		s.assertSingleOwner() // no panic
	})

	t.Run("same goroutine ok", func(t *testing.T) {
		s := New(nil, NewModuleConfig())
		s.assertOwner = true
		s.assertSingleOwner() // sets ownerGID
		s.assertSingleOwner() // same goroutine – ok
	})

	t.Run("different goroutine panics", func(t *testing.T) {
		s := New(nil, NewModuleConfig())
		s.assertOwner = true
		s.assertSingleOwner() // set owner to current goroutine

		result := make(chan bool, 1)
		go func() {
			defer func() {
				result <- recover() != nil
			}()
			s.assertSingleOwner() // different goroutine – must panic
		}()
		if panicked := <-result; !panicked {
			t.Error("expected panic on cross-goroutine assertSingleOwner")
		}
	})
}

// TestLogTrace covers the trace-enabled branch of logTrace.
func TestLogTrace(t *testing.T) {
	t.Parallel()
	s := New(nil, NewModuleConfig())
	s.trace = true
	s.logTrace("test value %d", 42) // should not panic
	s2 := New(nil, nil)
	s2.logTrace("should not print") // early return branch
}

// TestWASIReadBytesNilPaths covers the nil-return branches of readBytes.
func TestWASIReadBytesNilPaths(t *testing.T) {
	t.Parallel()
	s := New(nil, nil)
	if got := s.readBytes(0, 10); got != nil {
		t.Errorf("readBytes(0, 10) = %v, want nil", got)
	}
	if got := s.readBytes(10, 0); got != nil {
		t.Errorf("readBytes(10, 0) = %v, want nil", got)
	}
}

// TestResolvePath covers the mount-resolution logic.
func TestResolvePath(t *testing.T) {
	t.Parallel()

	s := New(nil,
		NewModuleConfig().WithReadOnlyFS("/", fstest.MapFS{}).WithReadOnlyFS("/lib", fstest.MapFS{}),
	)

	tests := []struct {
		input   string
		wantRel string
	}{
		{"/lib/5.16.3/Carp.pm", "5.16.3/Carp.pm"},
		{"/lib", ""},
		{"/", ""},
	}

	for _, tt := range tests {
		mount, rel := s.resolvePath(tt.input)
		if mount == nil {
			t.Errorf("resolvePath(%q): mount is nil", tt.input)
			continue
		}
		if rel != tt.wantRel {
			t.Errorf("resolvePath(%q): rel = %q, want %q", tt.input, rel, tt.wantRel)
		}
	}

	// A path that matches no mount returns nil.
	s2 := New(nil, NewModuleConfig().WithReadOnlyFS("/lib", fstest.MapFS{}))
	m, _ := s2.resolvePath("/usr/bin")
	if m != nil {
		t.Errorf("expected nil mount for unmatched path, got non-nil")
	}
}

// ReaddirDirent represents a single parsed WASI dirent entry.
type ReaddirDirent struct {
	Name   string
	Ino    uint64
	Type   byte
	Namlen uint32
	Next   uint64
}

// parseReaddirDirents parses a buffer of WASI dirent structs.
// Each dirent is: 8-byte d_next (uint64) + 8-byte d_ino (uint64) +
// 4-byte d_namlen (uint32) + 4-byte d_type (uint32) + name bytes.
// Stride per entry: 24 + d_namlen.
func parseReaddirDirents(buf []byte, bufUsed uint32, bufPtr int32) []ReaddirDirent {
	var entries []ReaddirDirent
	off := int(bufPtr)
	end := off + int(bufUsed)
	for off+24 <= end {
		dIno := binary.LittleEndian.Uint64(buf[off+8 : off+16])
		dNamlen := binary.LittleEndian.Uint32(buf[off+16 : off+20])
		dType := binary.LittleEndian.Uint32(buf[off+20 : off+24])
		name := string(buf[off+24 : off+24+int(dNamlen)])
		dNext := binary.LittleEndian.Uint64(buf[off : off+8])
		entries = append(entries, ReaddirDirent{
			Name:   name,
			Ino:    dIno,
			Type:   byte(dType),
			Namlen: dNamlen,
			Next:   dNext,
		})
		off += 24 + int(dNamlen)
	}
	return entries
}

// errorFS is a test [fs.FS] used to assert errno propagation from path_open:
// Open returns a [*fs.PathError] wrapping err for a matching path segment so
// [mapOSError] can classify it, for example [fs.ErrPermission] as EACCES. Other
// paths report not exist so resolution can reach the injected error.
type errorFS struct {
	name string
	err  error
}

func (e errorFS) Open(name string) (fs.File, error) {
	if name == e.name || strings.HasSuffix(name, "/"+e.name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: e.err}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func TestClockAndArgQueries(t *testing.T) {
	const (
		clockOff   = 100
		argcOff    = 200
		argvSzOff  = 208
		argvOff    = 300
		argvBufOff = 400
	)

	t.Run("Xclock_time_get realtime returns value > Jan 1 2020", func(t *testing.T) {
		s, buf := newTestState(nil)
		errno := s.Xclock_time_get(0, 0, clockOff)
		if errno != wasiESuccess {
			t.Fatalf("Xclock_time_get(0) returned %d, want ESUCCESS", errno)
		}
		got := binary.LittleEndian.Uint64(buf[clockOff : clockOff+8])
		const jan2020 = uint64(1577836800000000000) // Jan 1, 2020 in nanoseconds
		if got <= jan2020 {
			t.Errorf("Xclock_time_get(0) wrote %d, want > %d", got, jan2020)
		}
	})

	t.Run("Xclock_time_get monotonic is non-decreasing", func(t *testing.T) {
		s, buf := newTestState(NewModuleConfig().WithSysNanotime())
		errno := s.Xclock_time_get(1, 0, clockOff)
		if errno != wasiESuccess {
			t.Fatalf("Xclock_time_get(1) returned %d, want ESUCCESS", errno)
		}
		v1 := binary.LittleEndian.Uint64(buf[clockOff : clockOff+8])
		_ = s.Xclock_time_get(1, 0, clockOff)
		v2 := binary.LittleEndian.Uint64(buf[clockOff : clockOff+8])
		if v2 < v1 {
			t.Errorf("monotonic decreased: %d then %d", v1, v2)
		}
	})

	t.Run("Xclock_time_get unknown clock returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		errno := s.Xclock_time_get(99, 0, clockOff)
		if errno != wasiENoSys {
			t.Errorf("got %d, want ENOSYS (%d)", errno, wasiENoSys)
		}
	})

	t.Run("Xclock_res_get known clocks write 1 and return ESUCCESS", func(t *testing.T) {
		for _, id := range []int32{0, 1} {
			s, buf := newTestState(nil)
			errno := s.Xclock_res_get(id, clockOff)
			if errno != wasiESuccess {
				t.Errorf("Xclock_res_get(%d) returned %d, want ESUCCESS", id, errno)
			}
			got := binary.LittleEndian.Uint64(buf[clockOff : clockOff+8])
			if got != 1 {
				t.Errorf("Xclock_res_get(%d) wrote %d, want 1", id, got)
			}
		}
	})

	t.Run("Xclock_res_get unknown clock returns ENOSYS", func(t *testing.T) {
		s, _ := newTestState(nil)
		errno := s.Xclock_res_get(99, clockOff)
		if errno != wasiENoSys {
			t.Errorf("got %d, want ENOSYS (%d)", errno, wasiENoSys)
		}
	})

	t.Run("Xargs_sizes_get writes count and buf_size", func(t *testing.T) {
		s, buf := newTestState(NewModuleConfig().WithArgs("prog", "--flag", "val"))
		errno := s.Xargs_sizes_get(argcOff, argvSzOff)
		if errno != wasiESuccess {
			t.Fatalf("Xargs_sizes_get returned %d, want ESUCCESS", errno)
		}
		count := binary.LittleEndian.Uint32(buf[argcOff : argcOff+4])
		size := binary.LittleEndian.Uint32(buf[argvSzOff : argvSzOff+4])
		if count != 3 {
			t.Errorf("got count %d, want 3", count)
		}
		if size != 16 {
			t.Errorf("got buf_size %d, want 16", size)
		}
	})

	t.Run("Xargs_get writes pointer array and null-terminated strings", func(t *testing.T) {
		s, buf := newTestState(NewModuleConfig().WithArgs("prog", "--flag", "val"))
		_ = s.Xargs_get(argvOff, argvBufOff)
		// Check pointer array
		p0 := binary.LittleEndian.Uint32(buf[argvOff : argvOff+4])
		p1 := binary.LittleEndian.Uint32(buf[argvOff+4 : argvOff+8])
		p2 := binary.LittleEndian.Uint32(buf[argvOff+8 : argvOff+12])
		if p0 != uint32(argvBufOff) {
			t.Errorf("p0 = %d, want %d", p0, argvBufOff)
		}
		if p1 != uint32(argvBufOff+5) { // "prog\0" = 5 bytes
			t.Errorf("p1 = %d, want %d", p1, argvBufOff+5)
		}
		if p2 != uint32(argvBufOff+12) { // "prog\0"+"--flag\0" = 5+7 = 12
			t.Errorf("p2 = %d, want %d", p2, argvBufOff+12)
		}
		// Check strings
		prog := string(buf[argvBufOff : argvBufOff+5])
		flag := string(buf[argvBufOff+5 : argvBufOff+12])
		val := string(buf[argvBufOff+12 : argvBufOff+16])
		if prog != "prog\x00" {
			t.Errorf("got prog %q, want %q", prog, "prog\x00")
		}
		if flag != "--flag\x00" {
			t.Errorf("got flag %q, want %q", flag, "--flag\x00")
		}
		if val != "val\x00" {
			t.Errorf("got val %q, want %q", val, "val\x00")
		}
	})
}
