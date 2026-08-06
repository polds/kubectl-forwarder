package main

import "runtime/debug"

// devVersion is the placeholder carried by an unreleased tree.
const devVersion = "0.0.0"

// version is the released version of this binary. Release Please rewrites the
// literal below on every release PR (see release-please-config.json), so a
// tagged commit — and therefore `go install ...@vX.Y.Z` — always reports the
// right number. Release builds also inject it via -ldflags "-X main.version=".
var version = devVersion // x-release-please-version

// versionString reports the build version. The literal above wins whenever it
// has been set, either by Release Please on the tagged commit or by -ldflags at
// build time. Only an unreleased tree falls through to VCS build info, which Go
// stamps with a pseudo-version derived from the checkout.
func versionString() string {
	if version != devVersion {
		return "v" + version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "v" + version
}
