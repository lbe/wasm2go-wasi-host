package main

import (
	"reflect"
	"testing"
)

func TestParseImports(t *testing.T) {
	t.Run("single import", func(t *testing.T) {
		src := `package main
func New(v0 Xwasi_snapshot_preview1) *Module { return nil }`
		want := []string{"Xwasi_snapshot_preview1"}
		got, err := parseImports(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("multiple imports", func(t *testing.T) {
		src := `package main
func New(v0 Xenv, v1 Xwasi_snapshot_preview1) *Module { return nil }`
		want := []string{"Xenv", "Xwasi_snapshot_preview1"}
		got, err := parseImports(src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("missing New function", func(t *testing.T) {
		src := `package main
func NotNew() {}`
		_, err := parseImports(src)
		if err == nil {
			t.Error("expected error when New function is missing, got nil")
		}
	})
}
