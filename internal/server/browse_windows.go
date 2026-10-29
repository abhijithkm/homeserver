//go:build windows

package server

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// listRoots returns the drives a share can live on (fixed, removable, network).
func listRoots() []browseEntry {
	out := []browseEntry{}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return out
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		switch windows.GetDriveType(p) {
		case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_REMOTE:
			out = append(out, browseEntry{Name: root, Path: root, Readable: readableDir(root)})
		}
	}
	return out
}

// hiddenDir reports hidden or system folders ($Recycle.Bin, System Volume
// Information, ...), which are never useful as a share.
func hiddenDir(fi os.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	return d.FileAttributes&(syscall.FILE_ATTRIBUTE_HIDDEN|syscall.FILE_ATTRIBUTE_SYSTEM) != 0
}
