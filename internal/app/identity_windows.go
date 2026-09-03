//go:build windows

package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func fileIdentity(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	r1, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandle").Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return "", callErr
	}
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
func sameVolume(a, b string) bool {
	return strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b))
}
