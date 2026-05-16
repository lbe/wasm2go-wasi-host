package main

import (
	"runtime/debug"
)

func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	moduleVersion := info.Main.Version
	revision := ""
	modified := false

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	return formatVersion(moduleVersion, revision, modified)
}

func formatVersion(moduleVersion, revision string, modified bool) string {
	if moduleVersion != "" && moduleVersion != "(devel)" && !modified {
		return moduleVersion
	}

	version := "dev"
	if revision == "" {
		return version
	}

	shortRevision := revision
	if len(shortRevision) > 7 {
		shortRevision = shortRevision[:7]
	}

	if modified {
		return version + " (" + shortRevision + ", dirty)"
	}
	return version + " (" + shortRevision + ")"
}
