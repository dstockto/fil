package main

import "runtime/debug"

// Version is set at build time via -ldflags "-X main.Version=v1.2.3".
// It defaults to "dev" for local development builds.
// If still "dev" at runtime, resolveVersion falls back to the module
// version embedded by `go install`.
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
}
