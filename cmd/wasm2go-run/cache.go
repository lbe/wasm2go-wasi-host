package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	cacheSubdirTranspile = "transpile"
	cacheSubdirBuild     = "build"
	cacheFileModule      = "module.go"
	cacheFileMeta        = "meta.json"
	cacheFileLock        = ".lock"
)

// cacheEnabled reports whether tier-1 transpile caching is enabled.
// The cache is disabled when the -cache flag is set to "off" or when
// the WASM2GO_RUN_CACHE environment variable is "0", "false", or "off"
// (case-insensitive).
func cacheEnabled(cfg Config) bool {
	if cfg.Cache == "off" {
		return false
	}
	switch strings.ToLower(os.Getenv("WASM2GO_RUN_CACHE")) {
	case "0", "false", "off":
		return false
	}
	return true
}

// cacheKeyVersion is a monotonic integer that must be bumped whenever
// the transpile post-processing logic changes, invalidating prior cache
// entries.
var cacheKeyVersion = 1

// wasm2goIdentity returns a string that uniquely identifies the wasm2go
// toolchain version (e.g. its module version or build stamp). It is a
// variable so tests can swap in a stub.
var wasm2goIdentity = func() string {
	return "dev"
}

// hashOfFile reads path and returns a new SHA-256 hash of its contents.
// If the file cannot be read, err is non-nil.
func hashOfFile(path string) (hash.Hash, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, _ = h.Write(data)
	return h, nil
}

// transpileCacheKey returns a deterministic cache key for the WASM
// module at wasmPath. The key incorporates the file contents, the
// wasm2go toolchain identity, and the cache-key version. If the file
// cannot be read, an empty string is returned.
func transpileCacheKey(wasmPath string) string {
	h, err := hashOfFile(wasmPath)
	if err != nil {
		return ""
	}
	_, _ = fmt.Fprintf(h, "%s|%d", wasm2goIdentity(), cacheKeyVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// transpileCacheMeta holds the metadata written alongside a cached
// transpilation result.
type transpileCacheMeta struct {
	Wasm2goID          string `json:"wasm2goID"`
	PostprocessVersion int    `json:"postprocessVersion"`
	WasmSize           int64  `json:"wasmSize"`
}

// currentBuildCacheMeta returns metadata for a newly cached build entry
// derived from the current toolchain state and the given WASM module.
func currentBuildCacheMeta(wasmPath string) buildCacheMeta {
	m := buildCacheMeta{
		TranspileKey:        transpileCacheKey(wasmPath),
		BuildKeyVersion:     buildKeyVersion,
		WasiHostFingerprint: wasiHostFingerprint(),
		GoVersion:           goVersion,
		Goos:                goos,
		Goarch:              goarch,
		Wasm2goRunVersion:   wasm2goRunVersion,
	}
	m.Signature = m.computeSignature()
	return m
}

// buildCacheMeta holds the metadata written alongside a cached build result.
type buildCacheMeta struct {
	TranspileKey        string `json:"transpileKey"`
	BuildKeyVersion     int    `json:"buildKeyVersion"`
	WasiHostFingerprint string `json:"wasiHostFingerprint"`
	GoVersion           string `json:"goVersion"`
	Goos                string `json:"goos"`
	Goarch              string `json:"goarch"`
	Wasm2goRunVersion   string `json:"wasm2goRunVersion"`
	Signature           string `json:"signature"`
}

// currentTranspileCacheMeta returns metadata for a newly cached transpile
// entry using the active wasm2go identity and postprocess version.
func currentTranspileCacheMeta() transpileCacheMeta {
	return transpileCacheMeta{
		Wasm2goID:          wasm2goIdentity(),
		PostprocessVersion: cacheKeyVersion,
	}
}

// matchesCurrent reports whether meta reflects the active wasm2go identity
// and postprocess version.
func (m transpileCacheMeta) matchesCurrent() bool {
	return m.Wasm2goID == wasm2goIdentity() && m.PostprocessVersion == cacheKeyVersion
}

// readTranspileCacheMeta reads and parses the transpile cache metadata
// from dir. The second return value reports whether the metadata file
// exists and contains valid JSON.
func readTranspileCacheMeta(dir string) (transpileCacheMeta, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFileMeta))
	if err != nil {
		return transpileCacheMeta{}, false
	}
	var meta transpileCacheMeta
	if err = json.Unmarshal(data, &meta); err != nil {
		return transpileCacheMeta{}, false
	}
	return meta, true
}

// ensureTranspileCacheEntryDir creates the tier-1 transpile cache entry
// directory for key if it does not already exist.
func ensureTranspileCacheEntryDir(key string) (string, error) {
	dir := cacheTranspileEntryPath(key)
	return dir, os.MkdirAll(dir, 0755)
}

// removeTranspileCacheEntryDir deletes a tier-1 cache entry directory,
// discarding any lock file or partial artifacts from a failed population.
func removeTranspileCacheEntryDir(dir string) {
	_ = os.RemoveAll(dir)
}

// acquireExclusiveLock opens lockPath and acquires an exclusive flock. The
// returned unlock function releases the lock and closes the file.
func acquireExclusiveLock(lockPath string) (unlock func(), err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}

// withTranspileCachePopulateLock serializes cache population for key. The
// caller's populate runs only after an exclusive lock is acquired and a
// second cache lookup still misses. Concurrent waiters block until the
// writer finishes, then read the populated entry.
func withTranspileCachePopulateLock(key string, populate func() (string, error)) (string, error) {
	dir, err := ensureTranspileCacheEntryDir(key)
	if err != nil {
		return "", err
	}
	unlock, err := acquireExclusiveLock(filepath.Join(dir, cacheFileLock))
	if err != nil {
		return "", err
	}
	defer unlock()

	if src, hit := cacheGetTranspile(key); hit {
		return src, nil
	}
	src, err := populate()
	if err != nil {
		removeTranspileCacheEntryDir(dir)
		return "", err
	}
	return src, nil
}

// cachePutTranspile stores the transpiled Go source and its metadata in
// the tier-1 cache under the given key.
func cachePutTranspile(key, src string, meta transpileCacheMeta) error {
	dir, err := ensureTranspileCacheEntryDir(key)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, cacheFileModule), []byte(src), 0644); err != nil {
		return err
	}
	return writeCacheMeta(dir, meta)
}

// cacheGetTranspile retrieves the transpiled Go source for the given
// cache key. The second return value reports whether a cache hit
// occurred. A hit requires both the metadata and module files to exist
// and the metadata must match the current wasm2go identity and
// postprocess version.
func cacheGetTranspile(key string) (string, bool) {
	dir := cacheTranspileEntryPath(key)
	meta, ok := readTranspileCacheMeta(dir)
	if !ok || !meta.matchesCurrent() {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, cacheFileModule))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// cacheTranspileEntryPath returns the filesystem directory for the
// cache entry identified by key.
func cacheTranspileEntryPath(key string) string {
	return filepath.Join(cacheDir(), cacheSubdirTranspile, key)
}

// cacheBuildEntryPath returns the filesystem directory for the tier-2
// build cache entry identified by key.
func cacheBuildEntryPath(key string) string {
	return filepath.Join(cacheDir(), cacheSubdirBuild, key)
}

// ensureBuildCacheEntryDir creates the tier-2 build cache entry directory
// for key if it does not already exist.
func ensureBuildCacheEntryDir(key string) (string, error) {
	dir := cacheBuildEntryPath(key)
	return dir, os.MkdirAll(dir, 0755)
}

// readBuildCacheMeta reads and parses the build cache metadata from dir.
// The second return value reports whether the metadata file exists and
// contains valid JSON.
func readBuildCacheMeta(dir string) (buildCacheMeta, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFileMeta))
	if err != nil {
		return buildCacheMeta{}, false
	}
	var meta buildCacheMeta
	if err = json.Unmarshal(data, &meta); err != nil {
		return buildCacheMeta{}, false
	}
	return meta, true
}

// writeCacheMeta marshals meta to JSON and writes it as cacheFileMeta
// inside dir.
func writeCacheMeta(dir string, meta any) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheFileMeta), b, 0644)
}

// withBuildCachePopulateLock serializes tier-2 cache population for key.
// The caller's populate runs only after an exclusive lock is acquired and a
// second cache lookup still misses. Concurrent waiters block until the
// writer finishes, then read the already-populated entry.
func withBuildCachePopulateLock(key string, populate func(dir string) error) error {
	dir, err := ensureBuildCacheEntryDir(key)
	if err != nil {
		return err
	}
	unlock, err := acquireExclusiveLock(filepath.Join(dir, cacheFileLock))
	if err != nil {
		return err
	}
	defer unlock()

	if _, hit := cacheGetBuild(key); hit {
		return nil
	}
	if err := populate(dir); err != nil {
		errRemove := os.RemoveAll(dir)
		_ = errRemove
		return err
	}
	return nil
}

// cachePutBuild stores the compiled runner binary and its metadata in the
// tier-2 cache under the given key.
func cachePutBuild(key string, runnerBytes []byte, meta buildCacheMeta) error {
	return withBuildCachePopulateLock(key, func(dir string) error {
		if err := os.WriteFile(filepath.Join(dir, compileBinaryName), runnerBytes, 0755); err != nil {
			return err
		}
		return writeCacheMeta(dir, meta)
	})
}

// buildStateFields returns the toolchain and host values that determine
// cache validity, in the order they appear in the signature hash.
func buildStateFields() []string {
	return []string{
		fmt.Sprintf("%d", buildKeyVersion),
		wasiHostFingerprint(),
		goVersion,
		goos,
		goarch,
		wasm2goRunVersion,
	}
}

// computeSignature returns a SHA-256 hex digest over the transpile key
// and the toolchain/host fields returned by buildStateFields.
func (m buildCacheMeta) computeSignature() string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|", m.TranspileKey)
	for _, v := range buildStateFields() {
		_, _ = fmt.Fprintf(h, "%s|", v)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// matchesCurrent reports whether the stored build cache metadata matches
// the current toolchain and host state.  Entries written without a
// signature (old cache format) are always accepted for backward
// compatibility.
func (m buildCacheMeta) matchesCurrent() bool {
	if m.Signature == "" {
		return true
	}
	if m.Signature != m.computeSignature() {
		return false
	}
	want := buildStateFields()
	got := []string{
		fmt.Sprintf("%d", m.BuildKeyVersion),
		m.WasiHostFingerprint,
		m.GoVersion,
		m.Goos,
		m.Goarch,
		m.Wasm2goRunVersion,
	}
	for i, v := range got {
		if v != want[i] {
			return false
		}
	}
	return true
}

// cacheGetBuild retrieves the compiled runner binary for the given cache key.
// The second return value reports whether a cache hit occurred.
func cacheGetBuild(key string) ([]byte, bool) {
	dir := cacheBuildEntryPath(key)
	meta, ok := readBuildCacheMeta(dir)
	if !ok || !meta.matchesCurrent() {
		return nil, false
	}
	runnerBytes, err := os.ReadFile(filepath.Join(dir, compileBinaryName))
	if err != nil {
		return nil, false
	}
	return runnerBytes, true
}

// buildKeyVersion is a monotonic integer that must be bumped whenever
// the runner build logic changes, invalidating prior tier-2 cache entries.
var buildKeyVersion = 1

// buildCacheKey returns a deterministic tier-2 build cache key for a WASM
// module and runner configuration.
func buildCacheKey(wasmPath string, cfg Config) string {
	absPath, err := filepath.Abs(wasmPath)
	if err != nil {
		return ""
	}
	h, err := hashOfFile(absPath)
	if err != nil {
		return ""
	}
	_, _ = fmt.Fprintf(h, "|%s|%d|%s|%s|%s|%s|%s",
		canonicalConfigJSON(cfg),
		buildKeyVersion,
		wasiHostFingerprint(),
		goVersion,
		goos,
		goarch,
		wasm2goRunVersion,
	)
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalConfigJSON returns a canonical JSON representation of the
// runner configuration for cache key purposes. The JSON is produced
// from a subset of fields that affect the build; the marshal cannot
// fail for this struct so the error is ignored.
func canonicalConfigJSON(cfg Config) string {
	type cacheKeyConfig struct {
		Env      []string   `json:"env,omitempty"`
		Dirs     []DirMount `json:"dirs,omitempty"`
		WasmPath string     `json:"wasmPath,omitempty"`
		WasmArgs []string   `json:"wasmArgs,omitempty"`
	}

	env := make([]string, len(cfg.Env))
	copy(env, cfg.Env)
	sort.Strings(env)

	dirs := make([]DirMount, len(cfg.Dirs))
	copy(dirs, cfg.Dirs)
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].Host != dirs[j].Host {
			return dirs[i].Host < dirs[j].Host
		}
		return dirs[i].Guest < dirs[j].Guest
	})

	b, _ := json.Marshal(cacheKeyConfig{
		Env:      env,
		Dirs:     dirs,
		WasmPath: cfg.WasmPath,
		WasmArgs: cfg.WasmArgs,
	})
	return string(b)
}

// wasiHostFingerprint returns a SHA-256 hex digest of the wasihost.go
// source file in the directory returned by wasiHostPath. An empty string
// is returned when the host directory or source file cannot be located.
// It is a variable so tests can substitute a stub.
var wasiHostFingerprint = func() string {
	dir := wasiHostPath()
	if dir == "" {
		return ""
	}
	h, err := hashOfFile(filepath.Join(dir, "wasihost.go"))
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

var (
	goVersion         = "go1.23"
	goos              = "linux"
	goarch            = "amd64"
	wasm2goRunVersion = "dev"
)

// cacheDir returns the directory where wasm2go-run stores cached transpile
// artifacts. Resolution order:
//  1. WASM2GO_RUN_CACHE_DIR environment variable
//  2. XDG_CACHE_HOME/wasm2go-run
//  3. $HOME/.cache/wasm2go-run
func cacheDir() string {
	if dir := os.Getenv("WASM2GO_RUN_CACHE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "wasm2go-run")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "wasm2go-run")
}
