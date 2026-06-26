//go:build linux

// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package main

import "net"

func connectUnixDgram(path string) (*net.UnixConn, error) {
	return net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
}
