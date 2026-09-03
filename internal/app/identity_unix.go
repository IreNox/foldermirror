//go:build !windows

package app

import (
	"fmt"
	"os"
	"syscall"
)

func identityFromInfo(info os.FileInfo) (string, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("filesystem identity unavailable")
	}
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino), nil
}
func fileIdentity(path string) (string, error) {
	i, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return identityFromInfo(i)
}
func sameVolume(a, b string) bool {
	ai, e1 := os.Stat(a)
	bi, e2 := os.Stat(b)
	if e1 != nil || e2 != nil {
		return false
	}
	as, aok := ai.Sys().(*syscall.Stat_t)
	bs, bok := bi.Sys().(*syscall.Stat_t)
	return aok && bok && as.Dev == bs.Dev
}
