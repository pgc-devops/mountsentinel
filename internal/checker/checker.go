// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package checker

import (
	"bufio"
	"os"
	"strings"
)

type Mount struct {
	Device     string
	Mountpoint string
	FSType     string
	Options    []string
}

func (m Mount) IsReadOnly() bool {
	for _, opt := range m.Options {
		if opt == "ro" {
			return true
		}
	}
	return false
}

// ReadMounts parses /proc/mounts (or the provided path override).
func ReadMounts(path string) ([]Mount, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var mounts []Mount
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mounts = append(mounts, Mount{
			Device:     fields[0],
			Mountpoint: fields[1],
			FSType:     fields[2],
			Options:    strings.Split(fields[3], ","),
		})
	}
	return mounts, scanner.Err()
}

// IndexByMountpoint returns a map keyed by mountpoint.
func IndexByMountpoint(mounts []Mount) map[string]Mount {
	idx := make(map[string]Mount, len(mounts))
	for _, m := range mounts {
		idx[m.Mountpoint] = m
	}
	return idx
}

// IndexByDevice returns a map keyed by device (last entry wins for multiple mounts of same device).
func IndexByDevice(mounts []Mount) map[string]Mount {
	idx := make(map[string]Mount, len(mounts))
	for _, m := range mounts {
		idx[m.Device] = m
	}
	return idx
}
