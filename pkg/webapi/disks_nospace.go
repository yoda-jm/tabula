//go:build !linux && !darwin && !freebsd && !windows

package webapi

import "errors"

func diskSpace(path string) (free, total uint64, err error) {
	return 0, 0, errors.New("free space unknown on this system")
}

func writable(dir string) bool { return true }
