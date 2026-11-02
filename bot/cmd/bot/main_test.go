package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionFrom(t *testing.T) {
	const rev = "26ab92f95c27e6a2d9326e76f4ecf0b9cf5c02b4"
	tests := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"go run", []debug.BuildSetting{{Key: "GOOS", Value: "linux"}}, "devel"},
		{"clean tree", []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: "false"}}, "26ab92f95c27"},
		{"dirty tree", []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: rev}}, "26ab92f95c27+dirty"},
		{"short revision", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFrom(tt.settings); got != tt.want {
				t.Errorf("versionFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}
