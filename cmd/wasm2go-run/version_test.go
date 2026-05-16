package main

import "testing"

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name          string
		moduleVersion string
		revision      string
		modified      bool
		want          string
	}{
		{
			name:          "published semantic version",
			moduleVersion: "v1.2.3",
			want:          "v1.2.3",
		},
		{
			name:          "published pseudo version",
			moduleVersion: "v0.0.0-20260516204414-979865e5c948",
			want:          "v0.0.0-20260516204414-979865e5c948",
		},
		{
			name:          "development build without vcs metadata",
			moduleVersion: "(devel)",
			want:          "dev",
		},
		{
			name:          "development build with revision",
			moduleVersion: "(devel)",
			revision:      "abcdef1234567890",
			want:          "dev (abcdef1)",
		},
		{
			name:          "development build with short revision",
			moduleVersion: "(devel)",
			revision:      "abc123",
			want:          "dev (abc123)",
		},
		{
			name:          "dirty build reports dev even with module version",
			moduleVersion: "v1.2.3",
			revision:      "abcdef1234567890",
			modified:      true,
			want:          "dev (abcdef1, dirty)",
		},
		{
			name: "empty module version defaults to dev",
			want: "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.moduleVersion, tt.revision, tt.modified)
			if got != tt.want {
				t.Fatalf("formatVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionStringIsNonEmpty(t *testing.T) {
	if got := versionString(); got == "" {
		t.Fatal("versionString() is empty")
	}
}
