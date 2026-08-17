//go:build !linux

package fuse

func hostXattrLayout() XattrLayout { return XattrLayoutApple }
