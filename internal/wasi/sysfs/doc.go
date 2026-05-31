// Package sysfs implements wasi.FS and wasi.File adapters for host directories,
// read-only wrappers, and fs.FS mounts.
//
// Portions of this package are adapted from wazero internal/sysfs
// (https://github.com/tetratelabs/wazero), Copyright 2020-2023 The wazero Authors,
// licensed under the Apache License 2.0. See NOTICE at the repository root.
package sysfs
