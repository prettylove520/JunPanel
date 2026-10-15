//go:build linux

package main

import "syscall"

func diskSpace(path string) map[string]uint64 {
	var s syscall.Statfs_t
	if syscall.Statfs(path, &s) != nil {
		return map[string]uint64{}
	}
	return map[string]uint64{"total": s.Blocks * uint64(s.Bsize), "used": (s.Blocks - s.Bfree) * uint64(s.Bsize), "free": s.Bavail * uint64(s.Bsize)}
}
