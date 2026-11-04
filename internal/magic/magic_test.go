package magic

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lczyk/assert"
)

// minimalELF is a 64-bit little-endian x86-64 ELF header with one program
// header and nothing else: enough for file to call it an executable.
func minimalELF() []byte {
	b := make([]byte, 64+56)
	copy(b, "\x7fELF")
	b[4], b[5], b[6] = 2, 1, 1                // 64-bit, LSB, version 1
	binary.LittleEndian.PutUint16(b[16:], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(b[18:], 62) // EM_X86_64
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint64(b[24:], 0x400078) // entry
	binary.LittleEndian.PutUint64(b[32:], 64)       // phoff
	binary.LittleEndian.PutUint16(b[52:], 64)       // ehsize
	binary.LittleEndian.PutUint16(b[54:], 56)       // phentsize
	binary.LittleEndian.PutUint16(b[56:], 1)        // phnum
	binary.LittleEndian.PutUint16(b[58:], 64)       // shentsize
	binary.LittleEndian.PutUint32(b[64:], 1)        // PT_LOAD
	binary.LittleEndian.PutUint32(b[68:], 5)        // r-x
	binary.LittleEndian.PutUint64(b[80:], 0x400000) // vaddr
	binary.LittleEndian.PutUint64(b[96:], 120)      // filesz
	binary.LittleEndian.PutUint64(b[104:], 120)     // memsz
	binary.LittleEndian.PutUint64(b[112:], 0x1000)  // align
	return b
}

// rootfs makes a tree with one of everything the tool lists.
func rootfs(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	write := func(p string, content []byte, mode os.FileMode) {
		t.Helper()
		assert.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755))
		assert.NoError(t, os.WriteFile(filepath.Join(root, p), content, mode))
		assert.NoError(t, os.Chmod(filepath.Join(root, p), mode))
	}
	write("usr/bin/script", []byte("#!/bin/sh\necho hi\n"), 0755)
	write("usr/bin/py", []byte("#!/usr/bin/python3\nprint(1)\n"), 0755)
	write("usr/bin/elf", minimalELF(), 0755)
	write("usr/bin/suid", []byte("#!/bin/sh\n"), 0755|os.ModeSetuid)
	write("etc/conf", []byte("key = value\n"), 0644)
	write("etc/utf8", []byte("caf\xc3\xa9\n"), 0644)
	write("etc/empty", nil, 0644)
	write("usr/share/data", []byte{0, 1, 2, 3, 0xff, 0xfe}, 0644)
	assert.NoError(t, os.Symlink("script", filepath.Join(root, "usr/bin/sh")))
	assert.NoError(t, os.Symlink("/nope", filepath.Join(root, "usr/bin/dangling")))
	paths := []string{"/etc/", "/etc/conf", "/etc/empty", "/etc/utf8", "/usr/", "/usr/bin/", "/usr/bin/dangling", "/usr/bin/elf", "/usr/bin/py", "/usr/bin/script", "/usr/bin/sh", "/usr/bin/suid", "/usr/share/", "/usr/share/data"}
	return root, paths
}

func TestBuiltinList(t *testing.T) {
	b, err := NewBuiltin()
	assert.NoError(t, err)
	assert.Equal(t, b.Name(), "builtin")
	assert.ContainsString(t, b.Version(), "file 5.48")
	root, paths := rootfs(t)
	entries, err := List(b, root, paths)
	assert.NoError(t, err)
	var got []string
	for _, e := range entries {
		got = append(got, e.Path+": "+e.Line)
	}
	assert.EqualArrays(t, got, []string{
		"/etc/conf: ASCII text",
		"/etc/empty: empty",
		"/etc/utf8: Unicode text, UTF-8 text",
		"/usr/bin/dangling: symbolic link to /nope",
		"/usr/bin/elf: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, no section header",
		"/usr/bin/py: Python script, ASCII text executable",
		"/usr/bin/script: POSIX shell script, ASCII text executable",
		"/usr/bin/sh: symbolic link to script",
		"/usr/bin/suid: setuid POSIX shell script, ASCII text executable",
		"/usr/share/data: data",
	})
}

func TestListMissingPathIsALine(t *testing.T) {
	b, err := NewBuiltin()
	assert.NoError(t, err)
	entries, err := List(b, t.TempDir(), []string{"/nope"})
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.That(t, strings.HasPrefix(entries[0].Line, "error: "), entries[0].Line)
}

// TestPathMatchesBuiltin runs both backends over the same tree and wants
// the same raw lines. The host's file may be another release than the
// one softmagic implements, so a difference only fails when the versions
// match; otherwise it is logged.
func TestPathMatchesBuiltin(t *testing.T) {
	if _, err := exec.LookPath(FileBinary()); err != nil {
		t.Skipf("file binary not found (%v); set FILE=/path/to/file", err)
	}
	f, err := NewPath()
	assert.NoError(t, err)
	assert.Equal(t, f.Name(), "path")
	b, err := NewBuiltin()
	assert.NoError(t, err)
	root, paths := rootfs(t)
	fromFile, err := List(f, root, paths)
	assert.NoError(t, err)
	fromBuiltin, err := List(b, root, paths)
	assert.NoError(t, err)
	assert.Len(t, fromFile, len(fromBuiltin))
	same := strings.HasPrefix(f.Version(), "file-5.48")
	for i := range fromFile {
		assert.Equal(t, fromFile[i].Path, fromBuiltin[i].Path)
		if fromFile[i].Line == fromBuiltin[i].Line {
			continue
		}
		if same {
			t.Errorf("%s:\n  file:    %s\n  builtin: %s", fromFile[i].Path, fromFile[i].Line, fromBuiltin[i].Line)
		} else {
			t.Logf("%s (%s differs from file 5.48):\n  file:    %s\n  builtin: %s", fromFile[i].Path, f.Version(), fromFile[i].Line, fromBuiltin[i].Line)
		}
	}
}

func TestPick(t *testing.T) {
	b, err := Pick("builtin")
	assert.NoError(t, err)
	assert.Equal(t, b.Name(), "builtin")
	_, err = Pick("magic")
	assert.Error(t, err, "unknown backend")
	t.Setenv("FILE", "/nope/file")
	_, err = Pick("path")
	assert.Error(t, err, "no file binary")
	b, err = Pick("")
	assert.NoError(t, err)
	assert.Equal(t, b.Name(), "builtin")
}
