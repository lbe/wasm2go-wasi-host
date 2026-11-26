# Porting from wazero WASI Preview1 to wasm2go-wasi-host

This guide helps you move an existing Go application from [wazero](https://github.com/tetratelabs/wazero)'s WASI Preview1 runtime to [`wasm2go-wasi-host`](https://github.com/lbe/wasm2go-wasi-host). It assumes you have a working wazero embedding and want to switch to a **wasm2go-transpiled** guest with the corresponding bolt-on WASI host.

## Table of Contents

- [When to port](#when-to-port)
- [Key conceptual differences](#key-conceptual-differences)
- [Before and after: a minimal example](#before-and-after-a-minimal-example)
- [Step-by-step porting checklist](#step-by-step-porting-checklist)
- [API mapping](#api-mapping)
- [Filesystem and preopens](#filesystem-and-preopens)
- [Command model vs reactor model](#command-model-vs-reactor-model)
- [Error handling and proc_exit](#error-handling-and-proc_exit)
- [Common pitfalls](#common-pitfalls)
- [Validation](#validation)
- [Further reading](#further-reading)

---

## When to port

Porting makes sense when:

- You are already using (or willing to use) [`wasm2go`](https://github.com/ncruces/wasm2go) to transpile your Wasm guest into a native Go package.
- You want to eliminate the wazero runtime dependency from your embedding application.
- Your guest is single-threaded and you can accept the **single-goroutine ownership** contract of `wasm2go-wasi-host`.
- You want direct Go method calls into the guest instead of dynamic `api.Function.Call` dispatch.

If you need a general-purpose Wasm runtime that can load arbitrary `.wasm` binaries at runtime, wazero remains the better choice.

---

## Key conceptual differences

| Concern | wazero | wasm2go-wasi-host |
|---------|--------|-------------------|
| **Runtime** | `wazero.Runtime` manages engine, store, and compilation | No runtime; host is a plain `*wasihost.State` struct |
| **Guest code** | Wasm binary (`[]byte`) loaded dynamically | Go source code generated ahead-of-time by `wasm2go` |
| **Instantiation** | `r.InstantiateWithConfig(ctx, wasm, config)` | `generated.New(state, state)` — plain struct allocation |
| **WASI setup** | `wasi_snapshot_preview1.MustInstantiate(ctx, r)` | Implicit; `State` implements `X…` prefixed WASI methods |
| **Memory access** | `api.Memory` interface with typed reads/writes | Raw `[]byte` slice via callback re-invoked on every syscall |
| **Function calls** | `mod.ExportedFunction(name).Call(ctx, ...uint64)` | Direct Go method calls (`mod.X_start()`) with native types |
| **Start function** | Auto-invoked during instantiation (default `_start`) | Caller must invoke explicitly |
| **proc_exit** | Closes module, returns `*sys.ExitError` | Panics `wasihost.ExitError`; embedder recovers |

---

## Before and after: a minimal example

### wazero (before)

```go
package main

import (
    "context"
    "os"

    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
    "github.com/tetratelabs/wazero/sys"
)

func main() {
    ctx := context.Background()
    r := wazero.NewRuntime(ctx)
    defer r.Close(ctx)

    wasi_snapshot_preview1.MustInstantiate(ctx, r)

    wasm, _ := os.ReadFile("guest.wasm")

    mod, err := r.InstantiateWithConfig(ctx, wasm,
        wazero.NewModuleConfig().
            WithArgs("guest", "arg1").
            WithEnv("KEY", "value").
            WithStdout(os.Stdout).
            WithStderr(os.Stderr).
            WithFSConfig(wazero.NewFSConfig().
                WithDirMount("./data", "/data")).
            WithSysWalltime().
            WithSysNanotime())

    if exitErr, ok := err.(*sys.ExitError); ok {
        os.Exit(int(exitErr.ExitCode()))
    }
    if err != nil {
        panic(err)
    }
    _ = mod
}
```

### wasm2go-wasi-host (after)

```go
package main

import (
    "os"

    wasihost "github.com/lbe/wasm2go-wasi-host"
    generated "example.com/myguest" // output of wasm2go
)

func main() {
    var mod *generated.Module

    cfg := wasihost.NewModuleConfig().
        WithArgs("guest", "arg1").
        WithEnv("KEY", "value").
        WithStdout(os.Stdout).
        WithStderr(os.Stderr).
        WithHostDirectoryPreopen("/data", "./data").
        WithSysWalltime().
        WithSysNanotime()

    state := wasihost.New(
        func() []byte { return *mod.Xmemory().Slice() },
        cfg,
    )

    mod = generated.New(state, state)

    defer func() {
        if r := recover(); r != nil {
            if e, ok := r.(wasihost.ExitError); ok {
                os.Exit(int(e.Code))
            }
            panic(r)
        }
    }()

    mod.X_start()
}
```

---

## Step-by-step porting checklist

### 1. Transpile the guest with wasm2go

Replace runtime Wasm loading with ahead-of-time transpilation:

```bash
wasm2go -o generated_guest.go guest.wasm
```

The generated package exports:
- `type Module struct{ … }`
- `func New(imod imports.Imports, imem imports.Memory) *Module`
- `func (m *Module) Xmemory() *Memory`
- `func (m *Module) X_start()` (or whatever exports the guest defines)

### 2. Replace runtime and WASI instantiation

Delete:
- `wazero.NewRuntime(ctx)`
- `wasi_snapshot_preview1.MustInstantiate(ctx, r)`
- `r.InstantiateWithConfig(ctx, wasm, config)`

Add:
- Create a `wasihost.ModuleConfig` with the same argv, env, stdio, and clock settings.
- Create a `wasihost.State` with a memory callback.
- Instantiate the generated module with `generated.New(state, state)`.

### 3. Wire the memory callback

The callback is re-invoked on every WASI import so that memory growth stays correct:

```go
state := wasihost.New(
    func() []byte { return *mod.Xmemory().Slice() },
    cfg,
)
```

Because `mod` must already exist for the callback to close over it, declare `var mod *generated.Module` first, create the callback (which captures the pointer), then assign `mod = generated.New(state, state)`.

### 4. Port configuration

Translate wazero `ModuleConfig` / `FSConfig` methods to the corresponding `wasihost` builders (see [API mapping](#api-mapping) below).

### 5. Handle `proc_exit` explicitly

wazero can return `*sys.ExitError` from `InstantiateWithConfig`. In wasm2go-wasi-host, `proc_exit` panics with `wasihost.ExitError`. Recover at the call boundary:

```go
defer func() {
    if r := recover(); r != nil {
        if e, ok := r.(wasihost.ExitError); ok {
            os.Exit(int(e.Code))
        }
        panic(r)
    }
}()
mod.X_start()
```

### 6. Remove `context.Context` where it was only for wazero

wazero APIs are `context`-first. wasm2go-wasi-host does not take `context.Context` for syscalls; if your application passes `ctx` only because wazero required it, you can drop it.

### 7. Update reactor-style callers

If your application calls guest exports repeatedly (not just `_start`), replace:

```go
fn := mod.ExportedFunction("eval")
results, err := fn.Call(ctx, uint64(ptr), uint64(len))
```

with direct Go calls:

```go
result := mod.Xeval(ptr, len)
```

No `uint64` encoding, no dynamic lookup, no `api.Memory` reads/writes.

---

## API mapping

### Module configuration

| wazero | wasm2go-wasi-host | Notes |
|--------|-------------------|-------|
| `wazero.NewModuleConfig()` | `wasihost.NewModuleConfig()` | Both are immutable builders |
| `.WithArgs("a", "b")` | `.WithArgs("a", "b")` | Identical |
| `.WithEnv(k, v)` | `.WithEnv(k, v)` | Identical |
| `.WithStdin(r)` | `.WithStdin(r)` | Identical |
| `.WithStdout(w)` | `.WithStdout(w)` | Identical |
| `.WithStderr(w)` | `.WithStderr(w)` | Identical |
| `.WithSysWalltime()` | `.WithSysWalltime()` | Identical intent |
| `.WithSysNanotime()` | `.WithSysNanotime()` | Identical intent |
| `.WithSysNanosleep()` | `.WithSysNanosleep()` | Identical intent |
| `.WithRandSource(r)` | `.WithRandSource(r)` | Identical |
| `.WithOsyield(fn)` | `.WithOsyield(fn)` | Identical |
| `.WithWalltime(fn, res)` | `.WithWalltime(fn, res)` | Same signature shape |
| `.WithNanotime(fn, res)` | `.WithNanotime(fn, res)` | Same signature shape |
| `.WithName(name)` | `.WithName(name)` | Identical |
| `.WithStartFunctions("_start")` | *n/a* | Start functions are invoked explicitly by the caller |

### Filesystem configuration

| wazero | wasm2go-wasi-host | Notes |
|--------|-------------------|-------|
| `.WithFS(fs.FS)` | `.WithReadOnlyFS("/", fs)` | Root-mount convenience; wazero normalizes `/` to `""` |
| `.WithFSConfig(cfg)` | `.WithFS(cfg)` | Both accept a separate FS config object |
| `wazero.NewFSConfig().WithDirMount(dir, guestPath)` | `wasihost.NewFSConfig().WithDirMount(guestPath, dir)` | **Argument order is reversed** |
| `wazero.NewFSConfig().WithReadOnlyDirMount(dir, guestPath)` | `wasihost.NewFSConfig().WithReadOnlyDirMount(guestPath, dir)` | **Argument order is reversed** |
| `wazero.NewFSConfig().WithFSMount(fs, guestPath)` | `wasihost.NewFSConfig().WithFSMount(guestPath, fs)` | **Argument order is reversed** |
| `.WithFSConfig(...)` on a writable mount with overlay fallback | No overlay fallback | Use separate preopens for writable host dirs and read-only `fs.FS` mounts |

**Important:** wasm2go-wasi-host uses `(guestPath, hostPath)` order for mount methods, whereas wazero uses `(hostPath, guestPath)`. Double-check every mount call when porting.

### Convenience helpers

| wazero | wasm2go-wasi-host |
|--------|-------------------|
| `wazero.NewRuntime(ctx)` | *n/a* |
| `wasi_snapshot_preview1.MustInstantiate(ctx, r)` | *n/a* (implicit in `State`) |
| `r.InstantiateWithConfig(ctx, wasm, cfg)` | `generated.New(state, state)` |
| `mod.ExportedFunction(name).Call(ctx, ...uint64)` | `mod.X_name(...native types)` |
| `mod.Memory().ReadUint32Le(off)` | `binary.LittleEndian.Uint32(mem[off:])` |
| `mod.Memory().WriteUint32Le(off, v)` | `binary.LittleEndian.PutUint32(mem[off:], v)` |
| `mod.Close(ctx)` | *n/a* (garbage-collect the struct) |

---

## Filesystem and preopens

### Capability-oriented design

Both wazero and wasm2go-wasi-host are capability-oriented: the guest sees only what you preopen, and paths outside every preopen are inaccessible.

### Not a security boundary

wasm2go-wasi-host targets Go FFI use (calling a transpiled C or Rust library from Go), not sandboxing untrusted code. Preopens control what the guest can name; they are not an isolation guarantee. In particular, a root preopen of the host root, `WithHostDirectoryPreopen("/", "/")`, deliberately exposes the whole host filesystem to the guest so that guest paths and host paths are the same. Use it when the guest should behave like a native program; preopen narrower host directories when it should not. See [`path-confinement.md`](path-confinement.md) for the exact rules.

### One backend per mount

wasm2go-wasi-host **never** overlays a read-only `fs.FS` onto a writable host mount. If your guest needs both embedded assets and writable storage, mount them as **separate preopens**:

```go
cfg := wasihost.NewModuleConfig().
    WithReadOnlyFS("/assets", assetsFS).   // fd 3
    WithHostDirectoryPreopen("/tmp", "/var/tmp") // fd 4
```

### Path confinement

Both projects resolve guest paths against preopened roots. wasm2go-wasi-host documents its rules, including the root-preopen case, in [`path-confinement.md`](path-confinement.md). In practice, if your wazero embedding already used `WithDirMount` without escaping the root, the behavior should match.

### Rights

wasm2go-wasi-host seeds WASI rights automatically:
- Read-only mounts (`WithReadOnlyFS`, `WithFSMount`, `WithReadOnlyDirMount`) receive read-only rights.
- Writable host mounts (`WithHostDirectoryPreopen`, `WithDirMount`) receive writable rights.

You do not need to set rights manually unless you are doing advanced capability tuning.

---

## Command model vs reactor model

### Command model (`_start`)

This is the most common port. The change is mechanical:

```go
// wazero
mod, err := r.InstantiateWithConfig(ctx, wasm, cfg)
// err may be *sys.ExitError

// wasm2go-wasi-host
mod := generated.New(state, state)
defer recoverExit()
mod.X_start()
```

### Reactor model (exported functions)

Reactor-style guests keep state across calls. In wazero you look up and call exports dynamically:

```go
add := mod.ExportedFunction("add")
res, err := add.Call(ctx, api.EncodeI32(1), api.EncodeI32(2))
```

In wasm2go, exports are typed Go methods on the module struct:

```go
res := mod.Xadd(1, 2)
```

If your host code previously used `api.Memory` to read/write guest buffers, replace it with direct slice operations on the byte slice returned by your memory callback:

```go
mem := mod.Xmemory().Slice()
buf := (*mem)[ptr : ptr+len]
```

---

## Error handling and proc_exit

wazero surfaces `proc_exit` as an error returned from `InstantiateWithConfig` or `Function.Call`:

```go
if exitErr, ok := err.(*sys.ExitError); ok {
    os.Exit(int(exitErr.ExitCode()))
}
```

wasm2go-wasi-host surfaces it as a panic:

```go
defer func() {
    if r := recover(); r != nil {
        if e, ok := r.(wasihost.ExitError); ok {
            os.Exit(int(e.Code))
        }
        panic(r)
    }
}()
```

If your application previously checked `errors.Is(err, sys.ExitError{})` or similar, replace with `recover` and a type assertion.

---

## Common pitfalls

### 1. Memory callback timing

The memory callback **must** re-read the slice on every invocation because wasm2go may grow memory. Do not cache the `[]byte` across calls:

```go
// Good
state := wasihost.New(func() []byte { return *mod.Xmemory().Slice() }, cfg)

// Bad — stale slice after memory growth
mem := *mod.Xmemory().Slice()
state := wasihost.New(func() []byte { return mem }, cfg)
```

### 2. Mount argument order

wazero: `WithDirMount(hostDir, guestPath)`  
wasm2go-wasi-host: `WithDirMount(guestPath, hostDir)`

Swapping these is the most common source of "file not found" errors after porting.

### 3. No automatic start function

wazero calls `_start` (or your configured start functions) automatically. wasm2go-wasi-host does not. You must call `mod.X_start()` (or the relevant export) yourself.

### 4. Missing `WithSys*` clock calls

wasm2go-wasi-host defaults to **fake clocks** (walltime and nanotime return 0) if you do not opt in. wazero used to require explicit opt-in as well, but verify your config includes `.WithSysWalltime()` / `.WithSysNanotime()` / `.WithSysNanosleep()` if your guest needs real time.

### 5. Concurrency assumptions

`wasihost.State` is **not safe for concurrent use**. If your wazero embedding called guest functions from multiple goroutines (via `api.Function.Call`), you must add your own synchronization or create one `State` + `Module` per goroutine.

### 6. `fs.FS` does not imply overlay

If your wazero code relied on `WithFS(someFS)` combined with a writable directory mount on the same guest path, that behavior does not translate. Use separate preopens.

---

## Validation

After porting, validate with the same tests you ran against wazero. If you do not have integration tests, exercise at minimum:

1. **Argv and env**: `args_get`, `environ_get`
2. **Stdio**: `fd_write` to stdout/stderr, `fd_read` from stdin
3. **Clocks**: `clock_time_get`
4. **Filesystem**: `path_open`, `fd_read`, `fd_write`, `fd_seek`, `fd_readdir`, `path_filestat_get`
5. **Exit**: `proc_exit` returning the expected code

If you have access to [wasi-testsuite](https://github.com/WebAssembly/wasi-testsuite), run it through `wasm2go-run` to confirm the host layer itself is correct; any failures in your application port are then likely in configuration wiring.

---

## Further reading

- [`README.md`](../README.md) — Installation, usage, development
- [`ARCHITECTURE.md`](../ARCHITECTURE.md) — Syscall flow, VFS layer, source file map
- [`path-confinement.md`](path-confinement.md) — Path resolution and confinement rules
- [`wasm2go-wasi-host-wazero-wasi-design-rationale-comparison.md`](wasm2go-wasi-host-wazero-wasi-design-rationale-comparison.md) — Deep architectural comparison
- [`cmd/wasm2go-run/README.md`](../cmd/wasm2go-run/README.md) — CLI runner and tier-1 cache
