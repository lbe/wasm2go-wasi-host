package main

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantConfig Config
		wantErr    error
		wantStdout string
	}{
		{
			name: "full arguments",
			args: []string{"--env", "K=V", "--dir", "/h::/g", "foo.wasm", "arg1"},
			wantConfig: Config{
				Env:      []string{"K=V"},
				Dirs:     []DirMount{{Host: "/h", Guest: "/g"}},
				WasmPath: "foo.wasm",
				WasmArgs: []string{"arg1"},
			},
		},
		{
			name: "accumulate multiple env flags",
			args: []string{"--env", "A=1", "--env", "B=2", "foo.wasm"},
			wantConfig: Config{
				Env:      []string{"A=1", "B=2"},
				WasmPath: "foo.wasm",
			},
		},
		{
			name: "dir with single colon separator",
			args: []string{"--dir", "/h:/g", "foo.wasm"},
			wantConfig: Config{
				Dirs:     []DirMount{{Host: "/h", Guest: "/g"}},
				WasmPath: "foo.wasm",
			},
		},
		{
			name:    "missing wasm path",
			args:    []string{"--env", "K=V"},
			wantErr: errors.New("missing wasm path"),
		},
		{
			name:       "version requested",
			args:       []string{"--version"},
			wantErr:    ErrVersionRequested,
			wantStdout: "wasm2go-run ", // partial match; exact version comes from Go build metadata
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			gotConfig, err := parseConfig(tt.args, stdout)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantErr == nil || errors.Is(err, ErrVersionRequested) {
				if !reflect.DeepEqual(gotConfig, tt.wantConfig) {
					t.Errorf("Config mismatch\ngot:  %+v\nwant: %+v", gotConfig, tt.wantConfig)
				}
			}

			if tt.wantStdout != "" {
				if !bytes.Contains(stdout.Bytes(), []byte(tt.wantStdout)) {
					t.Errorf("stdout expected to contain %q, got %q", tt.wantStdout, stdout.String())
				}
			}
		})
	}
}
