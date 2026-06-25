//go:build linux

package main

import "net"

func connectUnixDgram(path string) (*net.UnixConn, error) {
	return net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
}
