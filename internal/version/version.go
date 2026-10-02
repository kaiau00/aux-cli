package version

import "runtime/debug"

// unknownVersion is what Version holds when the linker did not stamp one.
const unknownVersion = "unknown"

// Version is the version Aux reports. Set at build time with
// -ldflags "-X github.com/kaiau00/aux-cli/internal/version.Version=<v>",
// which is how goreleaser stamps the release tag.
var Version = unknownVersion

func init() {
	Version = resolveVersion(Version, debug.ReadBuildInfo)
}

// resolveVersion decides which version to report.
//
// A linker-stamped version always wins: that is the release's own identity, and
// it is the only value that corresponds to what a user actually downloaded.
// Embedded build info is a fallback for someone who ran
// `go install github.com/kaiau00/aux-cli@latest`, which passes no ldflags.
//
// That fallback used to apply unconditionally. It was harmless while `go build`
// left Main.Version empty, but Go 1.24 onwards stamps a VCS pseudo-version there
// for a plain `go build` too -- so it began firing on release binaries and
// overwriting the tag with something like v0.0.0-20261001171652-a2747cbfbef9.
// A release that misreports its own version is worse than no fallback, so the
// stamped value is now checked first.
func resolveVersion(stamped string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if stamped != unknownVersion {
		return stamped
	}

	info, ok := readBuildInfo()
	if !ok {
		// < go v1.18
		return stamped
	}

	switch info.Main.Version {
	case "", "(devel)":
		// Not built with `go install`; there is no version to recover.
		return stamped
	default:
		return info.Main.Version
	}
}
