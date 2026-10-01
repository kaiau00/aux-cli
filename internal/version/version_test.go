package version

import (
	"runtime/debug"
	"testing"
)

func buildInfo(mainVersion string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}, true
	}
}

// The release path, and the regression that prompted these tests: goreleaser
// stamps the tag through -ldflags, and nothing may overwrite it. Go 1.24 onwards
// stamps a VCS pseudo-version into build info even for a plain `go build`, so
// without this every released binary reported a pseudo-version instead of the
// release a user had downloaded.
func TestStampedVersionBeatsBuildInfo(t *testing.T) {
	got := resolveVersion("0.1.0", buildInfo("v0.0.0-20261001171652-a2747cbfbef9"))
	if got != "0.1.0" {
		t.Fatalf("the linker-stamped version must win, got %q", got)
	}
}

// The `go install github.com/kaiau00/aux-cli@latest` path: no ldflags, so the
// embedded build info is the only version available and is worth reporting.
func TestBuildInfoFillsInAnUnstampedVersion(t *testing.T) {
	got := resolveVersion(unknownVersion, buildInfo("v1.2.3"))
	if got != "v1.2.3" {
		t.Fatalf("expected the build-info version, got %q", got)
	}
}

// Placeholders carry no information and must not be reported as a version.
func TestUninformativeBuildInfoIsIgnored(t *testing.T) {
	for _, v := range []string{"", "(devel)"} {
		if got := resolveVersion(unknownVersion, buildInfo(v)); got != unknownVersion {
			t.Errorf("build info %q should be ignored, got %q", v, got)
		}
	}
}

// Reading build info can fail outright. Reporting the version is not worth a
// panic in that case.
func TestMissingBuildInfoIsSurvivable(t *testing.T) {
	got := resolveVersion(unknownVersion, func() (*debug.BuildInfo, bool) { return nil, false })
	if got != unknownVersion {
		t.Fatalf("expected %q, got %q", unknownVersion, got)
	}
}
