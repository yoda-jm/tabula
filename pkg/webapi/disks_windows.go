package webapi

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives   = kernel32.NewProc("GetLogicalDrives")
	getDriveType       = kernel32.NewProc("GetDriveTypeW")
	getDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// plugged lists the drives but the system's and the one holding the
// sessions directory: removable ones (USB sticks) and other disks (a USB
// hard disk shows as a fixed one).
func plugged(sessions string) []diskView {
	mask, _, _ := getLogicalDrives.Call()
	system := strings.ToUpper(os.Getenv("SystemDrive")) // C:
	var out []diskView
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":"
		dir := letter + `\`
		p, _ := syscall.UTF16PtrFromString(dir)
		kind, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(p)))
		if kind != 2 && kind != 3 || letter == system || strings.EqualFold(filepath.VolumeName(sessions), letter) {
			continue // 2 removable, 3 fixed
		}
		out = append(out, diskView{Path: dir, Name: letter, Writable: true})
	}
	return out
}

// diskSpace returns the bytes available to this user, and the size, of the
// volume holding path.
func diskSpace(path string) (free, total uint64, err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	if r, _, err := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), 0); r == 0 {
		return 0, 0, err
	}
	return free, total, nil
}
