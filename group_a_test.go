package wasihost

import (
	"encoding/binary"
	"io"
	"io/fs"
	"testing"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

type stubFSFile struct{}

func (stubFSFile) Read(_ []byte) (int, error) { return 0, io.EOF }
func (stubFSFile) Stat() (fs.FileInfo, error) { return nil, nil }
func (stubFSFile) Close() error               { return nil }

type errorWriter struct {
	err error
	n   int
}

func (w *errorWriter) Write(p []byte) (n int, err error) {
	if w.n > 0 {
		n = w.n
		if n > len(p) {
			n = len(p)
		}
	}
	return n, w.err
}

func newTestState(cfg *ModuleConfig) (*State, []byte) {
	buf := make([]byte, 65536)
	s := New(func() []byte { return buf }, cfg)
	return s, buf
}

func TestGroupAFoundation(t *testing.T) {
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
