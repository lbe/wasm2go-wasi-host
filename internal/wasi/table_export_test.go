// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

// Masks returns the masks of the table for testing purposes.
func Masks[Key ~int32, Item any](t *Table[Key, Item]) []uint64 {
	return t.masks
}
