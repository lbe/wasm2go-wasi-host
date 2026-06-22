# Path resolution and confinement

Guest filesystem access is limited to **preopened directories** and fds opened under them. Path handling lives mainly in `wasihost_path_resolve.go`; this document describes the rules enforced before host I/O.

## Preopen matching

- Each mount has a **guest path prefix** (e.g. `/`, `/data`) and a `wasi.FS` backend (`DirFS`, `AdaptFS`, or `ReadFS`).
- Guest-absolute paths use **longest-prefix match** among configured preopens (`resolvePath`).
- The match yields a **mount-relative** path (`rel`) used for VFS calls and, on writable host mounts, host-path joins.

## Resolving `(dirfd, path)`

`resolveDirfdPath` implements WASI directory-relative lookup:

| `path` | `dirfd` | Behavior |
|--------|---------|----------|
| Absolute (`/…`) | preopen or nested dir | `resolvePath` (dirfd ignored except non-preopen dirfd + abs → `EPERM`) |
| Relative | preopen dir fd | `rel` = path bytes under that preopen |
| Relative | nested directory fd | Join `FileEntry.GuestPath` with path, then `resolvePath` |
| Relative | non-directory fd | No preopen (`ENOENT` / `ENOTDIR` at syscall layer) |

Nested directory fds store the **guest-absolute** path at `path_open` time so recursive opens resolve correctly.

## Lexical confinement

Before writable host operations, mount-relative paths are checked for **lexical escape**:

- `preopenMountRelEscapes`: rejects `..` and `../segment` after `path.Clean`.
- Applied to **preopen dirfds** via `preopenDirfdLexicallyEscapes` → `ENOTCAPABLE`.
- **Non-preopen dirfds**: the resolved path relative to the owning preopen root must stay under the dirfd’s subtree within that preopen (`nonPreopenDirfdResolvedPathEscapes`) → `ENOTCAPABLE`. A directory fd opened from a root preopen (`guestPath == "/"`) therefore has `fdRel == ""` and is not treated as an escape.

These checks do not follow symlinks; they only block obvious `..` traversal in the guest path algebra.

## Writable host mounts (`DirFS`)

`preopenHostRoot` reads the host directory from `sysfs.DirFS` (or inner `DirFS` inside `ReadFS`). Mutations and some lookups build host paths with `filepath.Join(hostRoot, rel)`.

### Root preopen (`guestPath == "/"`)

`writableMountHostPaths` may use a **re-absolutized** path (`/` + rel) as primary and `Join(hostRoot, rel)` as fallback so absolute guest paths map to the intended host location.

With `LOOKUP_SYMLINK_FOLLOW`, `rootMountSymlinkFollowConfinementErrno` runs before returning those paths:

1. `EvalSymlinks` on the re-absolutized primary; on success, `resolvedPathConfinementErrno` against `hostRoot`.
2. If the primary is missing (`ErrNotExist`), `writableHostSymlinkFollowConfinementErrno` on the joined fallback under `hostRoot`.

A symlink that resolves outside the sandbox (for example `leak` → `../secret.txt`) returns `ENOTCAPABLE` or `ENOENT` from `writableMountHostPaths` and from `joinWritableHostPathForLookup` / `path_open` prechecks.

### Symlink follow (`LOOKUP_SYMLINK_FOLLOW`)

When follow is requested and the path has multiple segments (or guest rules require traversal), the host checks:

1. `filepath.EvalSymlinks` on the candidate host path (with parent walk-up when the final component is missing, e.g. `O_CREAT`).
2. `resolvedPathConfinementErrno`: resolved path must be **under** `EvalSymlinks(hostRoot)` → else `ENOTCAPABLE`.

When follow is **not** requested, `joinWritableHostPathForLookup` uses `Lstat` on the final component; a symlink there returns `ELOOP` (O_NOFOLLOW-style).

### Known limitation (TOCTOU)

Confinement uses one-shot `EvalSymlinks` before open. A symlink target may change before the subsequent open; closing that race needs per-component `openat(O_NOFOLLOW)`-style opens. See the TODO in `writableHostSymlinkFollowConfinementErrno`.

## VFS vs host path

| Operation | Read-only `AdaptFS` | Writable `DirFS` |
|-----------|---------------------|------------------|
| `path_open` / read / stat | `wasi.FS.OpenFile`, `Stat` | `FSContext.OpenFile` + host lookup helpers |
| `path_readlink` | `Readlink` on `ReadFS(DirFS)`; `AdaptFS` ENOSYS → `EROFS` | `resolveReadlinkLookup` + `wasi.FS.Readlink` (not `resolveWritableMutation`) |
| `path_*` mutations | `EROFS` / `ENOSYS` at FS layer | `wasi.FS` methods with `vfsPathForMutation` |
| Embedded-only file on writable mount | Separate read-only preopen recommended | Use `WithReadOnlyFS` / `WithFSMount` on a second preopen |

`path_readlink` is a read-only path operation: `resolveReadlinkLookup` mirrors `path_filestat_get` (dirfd resolution, writable confinement, `vfsPathForMutation`), then `Readlink`. Read-only `ReadFS(DirFS)` returns symlink targets from the inner mount; read-only `AdaptFS` maps `ENOSYS` to `EROFS`.

`PathFor()` on `DirFS` exposes the host root for confinement; virtual backends return `""` or a diagnostic string.

## Rights

Path and fd operations also enforce WASI **rights** masks on `FileEntry` (`RightsBase`, `RightsInheriting`) — see `wasihost_const.go` and `errnoIfFDRightsMissing`. Confinement is orthogonal: a path can be inside the sandbox but still return `ENOTCAPABLE` if the fd lacks the needed right.

## Related code

- `resolveWritable`, `resolveForOpen` — mutation and open entry points
- `wasihost_open.go` — `path_open` host join + `FSContext.OpenFile`
- `wasihost_path.go` — directory mutations after resolution
- [ARCHITECTURE.md](../ARCHITECTURE.md) — package layout and data flow
