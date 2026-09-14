package main

import "embed"

// The scenarios and the component directories they reference are embedded so
// the plugin is self-contained: a user who runs `cub plugin install
// confighub/cub-demo` has a binary and nothing else. Embedding also pins the
// dataset to the plugin version. The embed lives here, in package main at the
// repo root, because go:embed can only reference files at or below its own
// package directory and the directories are kept at the root so they read as
// content, not code.
//
//go:embed scenarios manifests
var assets embed.FS
