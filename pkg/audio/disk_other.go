//go:build !linux && !darwin && !freebsd && !windows

package audio

import "errors"

// FreeSpace is not known on this system.
func FreeSpace(path string) (uint64, error) {
	return 0, errors.New("free space unknown on this system")
}
