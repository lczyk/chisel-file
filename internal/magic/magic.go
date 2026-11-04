// Package magic describes files the way file(1) does, through the file
// binary on the host or a built-in port of libmagic, and normalises the
// descriptions so that architectures can be compared.
package magic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Backend answers what file(1) prints for regular files.
type Backend interface {
	// Name is "builtin" or "path".
	Name() string
	// Version names the libmagic whose answers these are.
	Version() string
	// Identify returns one line per path, in order. paths are absolute
	// paths in the rootfs (starting with /) and root is the rootfs; every
	// path is a regular file. A path that cannot be read gets an
	// "error: ..." line rather than failing the call.
	Identify(root string, paths []string) ([]string, error)
}

// Entry is one path of a rootfs and its line.
type Entry struct {
	Path string
	Line string
}

// Pick returns the backend for a --backend value: "builtin", "path", or ""
// for builtin.
func Pick(name string) (Backend, error) {
	switch name {
	case "", "builtin":
		return NewBuiltin()
	case "path":
		return NewPath()
	}
	return nil, fmt.Errorf("unknown backend %q, want builtin or path", name)
}

// FileBinary returns the file binary to run: $FILE, else "file" on PATH.
func FileBinary() string {
	if b := os.Getenv("FILE"); b != "" {
		return b
	}
	return "file"
}

// NewPath returns the path backend, the file binary on the host, or an
// error when none is found.
func NewPath() (Backend, error) {
	bin, err := exec.LookPath(FileBinary())
	if err != nil {
		return nil, fmt.Errorf("no file binary: %w (set FILE=/path/to/file, or drop --backend=path)", err)
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return nil, fmt.Errorf("%s --version: %w", bin, err)
	}
	version := string(out)
	if i := strings.IndexByte(version, '\n'); i >= 0 {
		version = version[:i]
	}
	return &pathBackend{bin: bin, version: version}, nil
}

// List describes every entry of a rootfs listing: regular files through the
// backend, symlinks as "symbolic link to <target>" from the tool itself,
// since file(1) would resolve the target against the host rather than the
// rootfs, and anything else (a fifo, a socket, a device) by file(1)'s word
// for it. Directories (paths ending in /) are left out. A path with a
// newline in it gets an error line, since the file binary takes one path
// per line. The result is sorted by path.
func List(b Backend, root string, paths []string) ([]Entry, error) {
	var regular []string
	var out []Entry
	for _, p := range paths {
		if len(p) == 0 || p[len(p)-1] == '/' {
			continue
		}
		if strings.ContainsRune(p, '\n') {
			out = append(out, Entry{Path: p, Line: "error: path contains a newline"})
			continue
		}
		info, err := os.Lstat(filepath.Join(root, p))
		switch {
		case err != nil:
			out = append(out, Entry{Path: p, Line: "error: " + err.Error()})
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(root, p))
			if err != nil {
				out = append(out, Entry{Path: p, Line: "error: " + err.Error()})
			} else {
				out = append(out, Entry{Path: p, Line: "symbolic link to " + target})
			}
		case info.Mode().IsRegular():
			regular = append(regular, p)
		default:
			out = append(out, Entry{Path: p, Line: special(info.Mode())})
		}
	}
	if len(regular) > 0 {
		lines, err := b.Identify(root, regular)
		if err != nil {
			return nil, err
		}
		if len(lines) != len(regular) {
			return nil, fmt.Errorf("%s: %d lines for %d paths", b.Name(), len(lines), len(regular))
		}
		for i, p := range regular {
			out = append(out, Entry{Path: p, Line: lines[i]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// special is file(1)'s word for a file that is not regular, a symlink or
// a directory.
func special(m os.FileMode) string {
	switch {
	case m&os.ModeNamedPipe != 0:
		return "fifo (named pipe)"
	case m&os.ModeSocket != 0:
		return "socket"
	case m&os.ModeCharDevice != 0:
		return "character special"
	case m&os.ModeDevice != 0:
		return "block special"
	}
	return m.Type().String()
}
