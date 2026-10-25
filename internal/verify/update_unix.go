//go:build !windows

package verify

import "os"

func replaceExecutable(current, staged string) (bool, error) {
	return false, os.Rename(staged, current)
}
