//go:build linux || darwin || freebsd

package audio

import "syscall"

// FreeSpace returns the bytes available to this user on the file system
// holding path.
func FreeSpace(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
