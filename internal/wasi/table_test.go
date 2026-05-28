package wasi

import (
	"testing"
)

func TestTable(t *testing.T) {
	// Insert 3 items → Lookup all 3 → Range visits all 3 → Delete all 3 → Len=0
	table := new(Table[int32, string])

	if n := table.Len(); n != 0 {
		t.Fatalf("new table length: got %d, want 0", n)
	}

	k0, ok := table.Insert("a")
	if !ok {
		t.Fatalf("Insert('a') returned ok=false")
	}
	k1, ok := table.Insert("b")
	if !ok {
		t.Fatalf("Insert('b') returned ok=false")
	}
	k2, ok := table.Insert("c")
	if !ok {
		t.Fatalf("Insert('c') returned ok=false")
	}

	// Lookup all 3
	v0, found := table.Lookup(k0)
	if !found {
		t.Errorf("Lookup(k0=%d): found=false, want true", k0)
	}
	if v0 != "a" {
		t.Errorf("Lookup(k0=%d): got %q, want %q", k0, v0, "a")
	}
	v1, found := table.Lookup(k1)
	if !found {
		t.Errorf("Lookup(k1=%d): found=false, want true", k1)
	}
	if v1 != "b" {
		t.Errorf("Lookup(k1=%d): got %q, want %q", k1, v1, "b")
	}
	v2, found := table.Lookup(k2)
	if !found {
		t.Errorf("Lookup(k2=%d): found=false, want true", k2)
	}
	if v2 != "c" {
		t.Errorf("Lookup(k2=%d): got %q, want %q", k2, v2, "c")
	}

	// Len should be 3 after 3 inserts
	if n := table.Len(); n != 3 {
		t.Errorf("Len after 3 inserts: got %d, want 3", n)
	}

	// Range visits all 3
	visited := make(map[int32]bool)
	rangeCount := 0
	table.Range(func(k int32, v string) bool {
		visited[k] = true
		rangeCount++
		return true
	})
	if rangeCount != 3 {
		t.Errorf("Range visited %d items, want 3", rangeCount)
	}
	for _, k := range []int32{k0, k1, k2} {
		if !visited[k] {
			t.Errorf("Range did not visit key %d", k)
		}
	}

	// Delete k0
	table.Delete(k0)
	if _, found := table.Lookup(k0); found {
		t.Errorf("Lookup(k0=%d) after Delete: found=true, want false", k0)
	}
	if n := table.Len(); n != 2 {
		t.Errorf("Len after 1 delete: got %d, want 2", n)
	}

	// Delete k1
	table.Delete(k1)
	if _, found := table.Lookup(k1); found {
		t.Errorf("Lookup(k1=%d) after Delete: found=true, want false", k1)
	}
	if n := table.Len(); n != 1 {
		t.Errorf("Len after 2 deletes: got %d, want 1", n)
	}

	// Delete k2
	table.Delete(k2)
	if _, found := table.Lookup(k2); found {
		t.Errorf("Lookup(k2=%d) after Delete: found=true, want false", k2)
	}
	if n := table.Len(); n != 0 {
		t.Errorf("Len after 3 deletes: got %d, want 0", n)
	}

	// InsertAt key -1 returns false
	if ok := table.InsertAt("x", -1); ok {
		t.Errorf("InsertAt(key=-1): got true, want false")
	}

	// Growth: Insert 32 items uses 1 mask
	t32 := new(Table[int32, string])
	for i := 0; i < 32; i++ {
		t32.Insert("x")
	}
	masks32 := Masks(t32)
	if len(masks32) != 1 {
		t.Errorf("Insert 32 items: masks length got %d, want 1", len(masks32))
	}

	// Growth: Insert 257 items uses 5 masks
	t257 := new(Table[int32, string])
	for i := 0; i < 257; i++ {
		t257.Insert("x")
	}
	masks257 := Masks(t257)
	if len(masks257) != 5 {
		t.Errorf("Insert 257 items: masks length got %d, want 5", len(masks257))
	}
}
