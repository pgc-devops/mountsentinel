// Copyright (c) 2026 CiteNet Internet. All rights reserved.
// Author: Michael Moscovitch
// SPDX-License-Identifier: MIT
// See the LICENSE file in the repository root for details.

package checker

import (
	"os"
	"testing"
)

const sampleMounts = `sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
/dev/sda1 / ext4 rw,relatime 0 0
/dev/sdb1 /data ext4 ro,relatime,errors=remount-ro 0 0
/dev/sdc1 /backup ext4 rw,relatime 0 0
`

func writeTempMounts(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mounts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestReadMounts(t *testing.T) {
	path := writeTempMounts(t, sampleMounts)
	mounts, err := ReadMounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 5 {
		t.Fatalf("expected 5 mounts, got %d", len(mounts))
	}
}

func TestIsReadOnly(t *testing.T) {
	path := writeTempMounts(t, sampleMounts)
	mounts, _ := ReadMounts(path)
	idx := IndexByMountpoint(mounts)

	if idx["/data"].IsReadOnly() != true {
		t.Error("/data should be read-only")
	}
	if idx["/"].IsReadOnly() != false {
		t.Error("/ should not be read-only")
	}
	if idx["/backup"].IsReadOnly() != false {
		t.Error("/backup should not be read-only")
	}
}

func TestIndexByMountpoint(t *testing.T) {
	path := writeTempMounts(t, sampleMounts)
	mounts, _ := ReadMounts(path)
	idx := IndexByMountpoint(mounts)

	if _, ok := idx["/data"]; !ok {
		t.Error("expected /data in index")
	}
}
