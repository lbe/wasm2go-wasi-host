// Copyright 2020-2023 The wazero Authors
//
// Portions of this file are adapted from wazero (Apache License 2.0):
// https://github.com/tetratelabs/wazero
//
// See NOTICE at the repository root for attribution and license terms.

package wasi

import (
	"math/bits"
	"slices"
)

// Table is a dense, growable map from 32-bit descriptor keys to items.
// Free slots are tracked via a bitmask (64 bits per word) for fast allocation.
type Table[Key ~int32, Item any] struct {
	masks []uint64
	items []Item
}

// Len returns the number of items stored in the table.
func (t *Table[Key, Item]) Len() (n int) {
	for _, mask := range t.masks {
		n += bits.OnesCount64(mask)
	}
	return n
}

// grow grows the table by n * 64 items.
func (t *Table[Key, Item]) grow(n int) {
	total := len(t.masks) + n
	t.masks = slices.Grow(t.masks, n)[:total]

	total = len(t.items) + n*64
	t.items = slices.Grow(t.items, n*64)[:total]
}

// Insert appends item to the first free descriptor slot and returns the
// assigned key. It returns false only when the allocated key overflows to a
// negative value (all int32 descriptors exhausted).
func (t *Table[Key, Item]) Insert(item Item) (key Key, ok bool) {
	for {
		for index, mask := range t.masks {
			if ^mask != 0 {
				shift := bits.TrailingZeros64(^mask)
				key = Key(index)*64 + Key(shift)
				t.items[key] = item
				t.masks[index] = mask | uint64(1<<shift)
				return key, key >= 0
			}
		}
		t.grow(1)
	}
}

// Lookup returns the item for key and true if the key is active.
// It returns the zero value and false for negative keys or unassigned descriptors.
func (t *Table[Key, Item]) Lookup(key Key) (item Item, found bool) {
	if key < 0 {
		return
	}
	i := int(key)
	if i >= len(t.items) {
		return
	}
	index := uint(key) / 64
	shift := uint(key) % 64
	if t.masks[index]&(1<<shift) != 0 {
		return t.items[i], true
	}
	return
}

// InsertAt stores item at the exact descriptor key.
// It returns false for negative keys; otherwise it grows the table as needed.
func (t *Table[Key, Item]) InsertAt(item Item, key Key) bool {
	if key < 0 {
		return false
	}
	index := uint(key) / 64
	if need := int(index) + 1 - len(t.masks); need > 0 {
		t.grow(need)
	}
	shift := uint(key) % 64
	t.masks[index] |= 1 << shift
	t.items[key] = item
	return true
}

// Delete removes the item at key, zeroing its slot and clearing the bit.
// Negative keys are a no-op.
func (t *Table[Key, Item]) Delete(key Key) {
	if key < 0 {
		return
	}
	index := uint(key) / 64
	if int(index) >= len(t.masks) {
		return
	}
	shift := uint(key) % 64
	mask := t.masks[index]
	if mask&(1<<shift) != 0 {
		var zero Item
		t.items[key] = zero
		t.masks[index] = mask &^ (1 << shift)
	}
}

// Range iterates every active (key, item) pair in ascending key order.
// Iteration stops early if f returns false.
func (t *Table[Key, Item]) Range(f func(Key, Item) bool) {
	for i, mask := range t.masks {
		if mask == 0 {
			continue
		}
		for j := Key(0); j < 64; j++ {
			if (mask & (1 << j)) == 0 {
				continue
			}
			if key := Key(i)*64 + j; !f(key, t.items[key]) {
				return
			}
		}
	}
}

// Reset clears the content of the table.
func (t *Table[Key, Item]) Reset() {
	clear(t.masks)
	clear(t.items)
}
