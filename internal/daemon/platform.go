package daemon

import "os"

func currentPID() int { return os.Getpid() }

var versionValue = func() string { return "dev" }

// SetVersion overrides the version reported by the status method.
func SetVersion(value string) {
	versionValue = func() string { return value }
}
