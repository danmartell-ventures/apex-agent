package version

import (
	"fmt"
	"runtime"
)

// Set via ldflags at build time.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func Full() string {
	return fmt.Sprintf("apex-agent %s (%s, %s) %s/%s", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}
