//go:build !linux

package main

func diskSpace(path string) map[string]uint64 { return map[string]uint64{} }
