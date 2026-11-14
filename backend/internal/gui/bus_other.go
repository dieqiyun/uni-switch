//go:build !linux

package gui

func sessionBus() bool { return true }

func dropTrayName() {}
