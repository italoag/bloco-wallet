package daemon

import "os"

func currentPID() int { return os.Getpid() }

var versionValue = func() string { return "dev" }
