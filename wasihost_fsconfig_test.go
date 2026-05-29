package wasihost

import (
	"reflect"
	"testing"
)

func TestFSConfig(t *testing.T) {
	// Check if NewFSConfig function exists
	newFSConfigFunc := reflect.ValueOf(NewFSConfig)
	if !newFSConfigFunc.IsValid() {
		t.Skip("NewFSConfig not yet implemented - expected failure in red phase")
	}

	cfg := NewFSConfig()
	if cfg == nil {
		t.Fatal("NewFSConfig() returned nil, expected non-nil *FSConfig")
	}

	t.Run("WithDirMount sets preopens with guest path", func(t *testing.T) {
		fs, paths := cfg.WithDirMount("/", "/tmp").preopens()
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen, got %d", len(fs))
		}
		if len(paths) != 1 {
			t.Fatalf("expected 1 guest path, got %d", len(paths))
		}
		// Guest path "/" is normalized to "" by StripPrefixesAndTrailingSlash
		if paths[0] != "" {
			t.Errorf("expected normalized guest path '', got %q", paths[0])
		}
	})

	t.Run("same guestPath mounted twice second wins", func(t *testing.T) {
		cfg := NewFSConfig().
			WithDirMount("/shared", "/tmp/first").
			WithDirMount("/shared", "/tmp/second")
		fs, paths := cfg.preopens()
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen, got %d", len(fs))
		}
		// Guest path "/shared" is normalized to "shared" by StripPrefixesAndTrailingSlash
		if paths[0] != "shared" {
			t.Errorf("expected normalized guest path 'shared', got %q", paths[0])
		}
		// The second mount wins, so the FS should be from /tmp/second
		if d, ok := fs[0].(interface{ PathFor() string }); ok {
			if d.PathFor() != "/tmp/second" {
				t.Errorf("expected FS path '/tmp/second', got %q", d.PathFor())
			}
		}
	})

	t.Run("nil guestPath becomes empty string", func(t *testing.T) {
		cfg := NewFSConfig().WithDirMount("", "/tmp")
		fs, paths := cfg.preopens()
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen, got %d", len(fs))
		}
		if paths[0] != "" {
			t.Errorf("expected empty guest path, got %q", paths[0])
		}
	})

	t.Run("guest path normalization", func(t *testing.T) {
		cfg := NewFSConfig().
			WithDirMount("/path/", "/tmp/a").
			WithDirMount("./path/", "/tmp/b").
			WithDirMount("path", "/tmp/c")
		fs, paths := cfg.preopens()
		// All three mounts should be deduplicated to a single normalized path "path"
		if len(fs) != 1 {
			t.Fatalf("expected 1 preopen after normalization and deduplication, got %d", len(fs))
		}
		if paths[0] != "path" {
			t.Errorf("expected normalized guest path 'path', got %q", paths[0])
		}
		// Last mount wins
		if d, ok := fs[0].(interface{ PathFor() string }); ok {
			if d.PathFor() != "/tmp/c" {
				t.Errorf("expected FS path '/tmp/c' (last mount wins), got %q", d.PathFor())
			}
		}
	})
}
