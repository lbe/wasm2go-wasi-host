package wasihost

import (
	"io/fs"

	"github.com/lbe/wasm2go-wasi-host/internal/wasi"
	"github.com/lbe/wasm2go-wasi-host/internal/wasi/sysfs"
)

// FSConfig holds filesystem configuration for a WASI module.
type FSConfig struct {
	fs              []wasi.FS
	guestPaths      []string // normalized keys for dedup and wasi.FSConfig()
	mountGuestPaths []string // original guest paths for State mount table / prestat names
	pathIndex       map[string]int
}

// NewFSConfig returns a pointer to an empty FSConfig.
func NewFSConfig() *FSConfig {
	return &FSConfig{
		pathIndex: make(map[string]int),
	}
}

// WithDirMount adds a writable host directory preopen. Parameters are in canonical
// (guestPath, hostPath) order for consistency with ModuleConfig.WithHostDirectoryPreopen.
func (c *FSConfig) WithDirMount(guestPath, hostPath string) *FSConfig {
	return c.withSysFS(sysfs.DirFS{Path: hostPath}, guestPath)
}

// WithReadOnlyDirMount adds a read-only host directory preopen. Parameters are in canonical
// (guestPath, hostPath) order for consistency with ModuleConfig.WithReadOnlyFS.
func (c *FSConfig) WithReadOnlyDirMount(guestPath, hostPath string) *FSConfig {
	return c.withSysFS(&sysfs.ReadFS{FS: sysfs.DirFS{Path: hostPath}}, guestPath)
}

// WithFSMount adds a read-only fs.FS preopen. Parameters are in canonical
// (guestPath, fsys) order for consistency with ModuleConfig.WithReadOnlyFS.
func (c *FSConfig) WithFSMount(guestPath string, fsys fs.FS) *FSConfig {
	return c.withSysFS(&sysfs.AdaptFS{FS: fsys}, guestPath)
}

// withSysFS is the internal method for adding any sys.FS mount.
func (c *FSConfig) withSysFS(fsys wasi.FS, guestPath string) *FSConfig {
	clone := c.clone()
	normalized := wasi.StripPrefixesAndTrailingSlash(guestPath)
	mountPath := guestPath
	if mountPath == "" {
		mountPath = "/"
	}

	// Duplicate guest path: second mount wins
	if idx, exists := clone.pathIndex[normalized]; exists {
		clone.fs[idx] = fsys
		clone.guestPaths[idx] = normalized
		clone.mountGuestPaths[idx] = mountPath
	} else {
		idx := len(clone.fs)
		clone.fs = append(clone.fs, fsys)
		clone.guestPaths = append(clone.guestPaths, normalized)
		clone.mountGuestPaths = append(clone.mountGuestPaths, mountPath)
		clone.pathIndex[normalized] = idx
	}
	return clone
}

// preopens returns the accumulated filesystems and normalized guest paths.
func (c *FSConfig) preopens() ([]wasi.FS, []string) {
	return c.fs, c.guestPaths
}

// mountPreopens returns filesystems with guest paths for the State mount table.
func (c *FSConfig) mountPreopens() ([]wasi.FS, []string) {
	return c.fs, c.mountGuestPaths
}

func (c *FSConfig) clone() *FSConfig {
	ret := &FSConfig{
		fs:              make([]wasi.FS, len(c.fs)),
		guestPaths:      make([]string, len(c.guestPaths)),
		mountGuestPaths: make([]string, len(c.mountGuestPaths)),
		pathIndex:       make(map[string]int, len(c.pathIndex)),
	}
	copy(ret.fs, c.fs)
	copy(ret.guestPaths, c.guestPaths)
	copy(ret.mountGuestPaths, c.mountGuestPaths)
	for k, v := range c.pathIndex {
		ret.pathIndex[k] = v
	}
	return ret
}
