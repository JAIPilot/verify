package verify

import "runtime"

func platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
