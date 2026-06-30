package version

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (%s, %s, %s/%s)", Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
}

func IsRelease() bool {
	return Version != "" && Version != "dev"
}
