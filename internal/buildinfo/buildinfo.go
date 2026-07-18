package buildinfo

// Version is replaced at build time with -ldflags.
var Version = "dev"

// Revision is the immutable source revision embedded by release builds.
var Revision = "unknown"
