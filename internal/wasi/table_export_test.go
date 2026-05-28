package wasi

// Masks returns the masks of the table for testing purposes.
func Masks[Key ~int32, Item any](t *Table[Key, Item]) []uint64 {
	return t.masks
}
