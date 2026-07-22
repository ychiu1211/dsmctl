package buildinfo

const (
	// CompatibilityTrain is the latest DSM feature release certified by this
	// source revision. Operation-level support is still selected at runtime
	// from the APIs advertised by each NAS.
	CompatibilityTrain = "7.3.2"
	// ReleaseBuild increases monotonically for dsmctl releases made from the
	// same DSM compatibility train.
	ReleaseBuild = 14
	// CurrentVersion follows DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD.
	CurrentVersion = "7.3.2-14"
)

// Version is replaced at build time with -ldflags.
var Version = CurrentVersion

// Revision is the immutable source revision embedded by release builds.
var Revision = "unknown"
