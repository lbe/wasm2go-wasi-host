# WASI Testsuite Close-Out Strategy

## Overview

Five tests remain failing after completing all planned code changes for
wasi-testsuite compliance. This document analyzes each failure, identifies
the root causes, and specifies the code changes needed.

## Current Pass/Fail Status

```
FAIL: 5/72 tests failed (40 skipped)
  file_seek_tell.wasm    (Rust)
  fd_advise.wasm         (Rust)
  file_allocate.wasm     (Rust)
  lseek.wasm             (C)
  pwrite-with-append.wasm (C)
```

All five failures stem from **two root causes**:

---

## Root Cause 1: File Offset Tracking Mismatch

**Affects:** file_seek_tell, lseek, pwrite-with-append

### The Problem

The WASI fd-table implementation uses two different mechanisms to track the
current file offset, and they are out of sync:

| Syscall | Offset source | Underlying mechanism |
|---------|--------------|---------------------|
| `fd_read`  | `entry.offset` | `ReadAt(buf, entry.offset)` — reads at tracked offset, kernel position unchanged |
| `fd_write` | `entry.offset` | `WriteAt(buf, entry.offset)` — writes at tracked offset, kernel position unchanged |
| `fd_seek`  | kernel position | `sk.Seek(offset, whence)` — uses OS kernel file position |
| `fd_tell`  | `entry.offset` | Returns `entry.offset` directly |

After `fd_read` or `fd_write` advances `entry.offset`, the OS kernel file
position is still at 0 (the file has never been explicitly seeked). When
a subsequent `fd_seek` with `WHENCE_CUR` calls `sk.Seek(0, SeekCurrent)`,
it returns the kernel position (0), not the WASI-tracked offset.

### How Each Test Is Affected

#### 1. file_seek_tell.wasm (Rust)

```rust
// fd_write 100 bytes → WriteAt(offset=0) → entry.offset=100, kernel pos=0
// fd_tell → entry.offset → 100 ✓
// fd_seek(-50, WHENCE_CUR) → sk.Seek(-50, SeekCurrent) → kernel pos=0
//   → tries to seek to -50 → EINVAL ✗ (expected 50)
```

**Expected:** `fd_seek(-50, WHENCE_CUR)` returns new offset 50
**Actual:** Returns EINVAL (kernel position is 0, cannot seek to -50)

#### 2. lseek.wasm (C)

```c
// fread(buf, 4) → fd_read → ReadAt(offset=0) → entry.offset=4, kernel pos=0
// lseek(fd, 0, SEEK_CUR) → fd_seek(0, WHENCE_CUR)
//   → sk.Seek(0, SeekCurrent) → kernel pos=0 → returns 0
```

**Expected:** `lseek(fd, 0, SEEK_CUR)` returns 4
**Actual:** Returns 0

#### 3. pwrite-with-append.wasm (C)

```c
// open(…, O_APPEND) → path_open with fdFlagsAppend
// write(fd, buf, 2) → fd_write → WriteAt(buf, fi.Size()=0) → entry.offset=2
// lseek(fd, 0, SEEK_SET) → fd_seek(0, WHENCE_SET) → kernel pos=0
// write(fd, buf, 2) → fd_write → WriteAt(buf, fi.Size()=2) → entry.offset=4
// lseek(fd, 0, SEEK_CUR) → fd_seek(0, WHENCE_CUR)
//   → sk.Seek(0, SeekCurrent) → kernel pos=0 → returns 0
```

**Expected:** `lseek(fd, 0, SEEK_CUR)` returns 4
**Actual:** Returns 0

### The Fix

**Replace `ReadAt`/`WriteAt` with `Seek`+`Read`/`Write` in `Xfd_read` and
`Xfd_write`.** This synchronizes the kernel file position with `entry.offset`
for every I/O operation.

#### For `fd_read` (in `Xfd_read`):

```go
// Before (broken):
ra, ok := entry.file.(interface{ ReadAt([]byte, int64) (int, error) })
if ok {
    n, err = ra.ReadAt(mem[bufPtr:bufPtr+bufLen], entry.offset)
}
entry.offset += int64(n)

// After (fixed):
sk, ok := entry.file.(io.Seeker)
if ok {
    sk.Seek(entry.offset, io.SeekStart)
}
n, err = entry.file.Read(mem[bufPtr : bufPtr+bufLen])
entry.offset += int64(n)
```

Key points:
- Seek to `entry.offset` before each iovec buffer read
- Use `entry.file.Read()` instead of `ReadAt` — this advances kernel position
- `entry.offset` is incremented by bytes read (same as before)
- After the loop, kernel position == `entry.offset`
- Handle non-Seekable files (e.g., stdin, FSFileWrap without seek) as before

#### For `fd_write` (in `Xfd_write`):

```go
// Before (broken):
writeOff := entry.offset
if entry.fdFlags&uint16(fdFlagsAppend) != 0 {
    if fi, err := entry.file.Stat(); err == nil {
        writeOff = fi.Size()
    }
}
n, err := wa.WriteAt(mem[bufPtr:bufPtr+bufLen], writeOff)
entry.offset += int64(n)

// After (fixed):
if entry.fdFlags&uint16(fdFlagsAppend) != 0 {
    // O_APPEND: os.File always writes at end; just use Write()
    n, err = entry.file.Write(mem[bufPtr : bufPtr+bufLen])
} else {
    sk, ok := entry.file.(io.Seeker)
    if ok {
        sk.Seek(entry.offset, io.SeekStart)
    }
    n, err = entry.file.Write(mem[bufPtr : bufPtr+bufLen])
}
entry.offset += int64(n)
```

For `osFile` (backing `*os.File`):
- `O_APPEND` was set at `path_open` time via `os.OpenFile` flags, so
  `os.File.Write()` always appends to end — no need to call `fi.Size()`
- Without append, Seek + Write updates kernel position naturally

For `FSFileWrap` (embedded read-only files):
- `fd_write` is not meaningful (would fail or have no backing write impl)
- Can fall through to existing behavior

#### For `fd_seek` with `WHENCE_CUR`:

After the above fix, `fd_seek` no longer needs changes — it already calls
`sk.Seek(offset, whence)` which will use the correct kernel position.

However, to be robust for non-Seekable files or edge cases, we should add a
fallback: when the file does not implement `io.Seeker`, compute the seek
from `entry.offset` directly:

```go
sk, ok := entry.file.(io.Seeker)
if !ok {
    // Fallback for non-seekable files: compute new offset from entry.offset
    switch whence {
    case io.SeekStart:
        entry.offset = offset
    case io.SeekCurrent:
        entry.offset += offset
    case io.SeekEnd:
        return wasiEInval // can't determine end without seek
    }
    if entry.offset < 0 {
        return wasiEInval
    }
    // Write new offset to guest memory
    binary.LittleEndian.PutUint64(s.mem()[newOffsetPtr:], uint64(entry.offset))
    return wasiESuccess
}
n, err := sk.Seek(offset, int(whence))
```

---

## Root Cause 2: `fd_allocate` Is a No-Op Stub

**Affects:** fd_advise.wasm, file_allocate.wasm

### The Problem

`Xfd_allocate` unconditionally returns `wasiESuccess` without actually
allocating disk space. The test suite expects that `fd_allocate` grows the
file if `offset + len > current file size`.

```go
// Current broken implementation:
func (s *State) Xfd_allocate(fd int32, offset, length int64) int32 {
    // ...validates fd... then:
    return wasiESuccess
}
```

#### fd_advise.wasm

```rust
wasi::fd_filestat_set_size(file_fd, 100).expect("setting size");
// file size is now 100

match wasi::fd_allocate(file_fd, 100, 100) {
    Ok(()) => {
        let stat = wasi::fd_filestat_get(file_fd).expect("failed to fdstat");
        assert_eq!(stat.size, 200, "file size should be 200"); // FAILS: size is still 100
    }
    Err(err) => {
        assert_eq!(err, wasi::ERRNO_NOTSUP, "allocating size");
    }
}
```

#### file_allocate.wasm

```rust
match wasi::fd_allocate(file_fd, 0, 100) {
    Ok(()) => {
        stat = wasi::fd_filestat_get(file_fd).expect("reading file stats");
        assert_eq!(stat.size, 100, "file size should be 100"); // FAILS: size is still 0

        wasi::fd_allocate(file_fd, 10, 10).expect("allocating size less than current size");
        stat = wasi::fd_filestat_get(file_fd).expect("reading file stats");
        assert_eq!(stat.size, 100, "file size should remain unchanged at 100");

        wasi::fd_allocate(file_fd, 90, 20).expect("allocating size larger than current size");
        stat = wasi::fd_filestat_get(file_fd).expect("reading file stats");
        assert_eq!(stat.size, 110, "file size should increase from 100 to 110");
    }
    Err(err) => {
        assert_eq!(err, wasi::ERRNO_NOTSUP, "allocating size");
    }
}
```

### The Fix

Implement `fd_allocate` to call `os.File.Truncate` with the new size
`max(current_size, offset + length)` when backed by an `osFile`. For
`FSFileWrap` (embedded read-only files), return `wasiENotSup`.

Per WASI spec, `fd_allocate`:
1. Allocates zero-fill space for the region `[offset, offset+length)`
2. If `offset` is past end-of-file, the file is first grown to `offset`
   (creating a hole or filling with zeros), then `length` bytes are allocated
3. The file size becomes `max(current_size, offset + length)`

```go
func (s *State) Xfd_allocate(fd int32, offset, length int64) int32 {
    if fd < 0 || int(fd) >= len(s.fds) {
        return wasiEBadf
    }
    entry := s.fds[fd]
    if entry.isUnused() {
        return wasiEBadf
    }
    if errno := errnoForDirectoryFDOp(entry); errno != 0 {
        return errno
    }
    if errno := errnoIfFDRightsMissing(entry.rightsBase, rightFDAllocate); errno != 0 {
        return errno
    }
    
    of, ok := entry.file.(*osFile)
    if !ok {
        return wasiENotSup
    }
    
    fi, err := of.Stat()
    if err != nil {
        return mapOSError(err)
    }
    
    newSize := offset + length
    if newSize < fi.Size() {
        newSize = fi.Size()
    }
    
    if err := of.Truncate(newSize); err != nil {
        return mapOSError(err)
    }
    return wasiESuccess
}
```

**Note:** `rightFDAllocate` must be present in the fd's `rightsBase` for this
to work. The `pathOpenStoredRights` function and preopen rights masks already
include this bit (via `rightsWritableDirPreopenInheriting`).

---

## Implementation Order

1. **Fix `Xfd_read`** — Seek then Read (affects file_seek_tell, lseek)
2. **Fix `Xfd_write`** — Seek then Write, handle APPEND (affects file_seek_tell,
   pwrite-with-append)
3. **Fix `Xfd_seek`** — Add non-Seekable fallback using `entry.offset` for
   robustness (affects all seek/tell operations)
4. **Fix `Xfd_allocate`** — Implement actual space allocation via Truncate
   (affects fd_advise, file_allocate)

---

## Testing Strategy

After implementing each fix:

1. Run the specific failing test via `go test -run <testname>`
2. Run the full `go test ./...` to detect regressions
3. Run `make test-wasi-testsuite` (or the full test script) to get the final
   pass/fail count

Expected result after all fixes: **0/72 tests failed** (40 skipped, 32 passed).

---

## Summary

| Test | Root Cause | Fix Location |
|------|-----------|-------------|
| `file_seek_tell.wasm` | Offset tracking mismatch (ReadAt/WriteAt vs kernel Seek) | `Xfd_read`, `Xfd_write` |
| `lseek.wasm` | Offset tracking mismatch | `Xfd_read` |
| `pwrite-with-append.wasm` | Offset tracking mismatch + O_APPEND not advancing kernel pos | `Xfd_write` |
| `fd_advise.wasm` | `fd_allocate` is a no-op | `Xfd_allocate` |
| `file_allocate.wasm` | `fd_allocate` is a no-op | `Xfd_allocate` |