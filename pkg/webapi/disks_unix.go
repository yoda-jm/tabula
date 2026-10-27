//go:build linux || darwin || freebsd

package webapi

import "syscall"

// diskSpace returns the bytes available to this user, and the size, of the
// file system holding path.
func diskSpace(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), nil
}

// writable reports whether this user may write in dir.
func writable(dir string) bool { return syscall.Access(dir, 2) == nil } // W_OK
