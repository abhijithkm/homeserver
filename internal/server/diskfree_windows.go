//go:build windows

package server

import "golang.org/x/sys/windows"

// diskFree returns free and total bytes of the volume holding path.
func diskFree(path string) (free, total uint64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var avail, tot, totFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totFree); err != nil {
		return 0, 0, false
	}
	return avail, tot, true
}
