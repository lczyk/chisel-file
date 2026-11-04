package magic

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// pathBackend runs the host's file binary, one process per rootfs.
type pathBackend struct {
	bin     string
	version string
}

func (f *pathBackend) Name() string    { return "path" }
func (f *pathBackend) Version() string { return f.version }

// Identify runs "file -N -0 -f -" over the paths. file acts on -f while
// parsing its options, so every other flag must come before it. With -0
// each line is "<name>\0: <description>", which makes the split exact
// however the name looks.
func (f *pathBackend) Identify(root string, paths []string) ([]string, error) {
	var in bytes.Buffer
	for _, p := range paths {
		in.WriteString(filepath.Join(root, p))
		in.WriteByte('\n')
	}
	cmd := exec.Command(f.bin, "-N", "-0", "-f", "-")
	cmd.Stdin = &in
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n  %s", f.bin, err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(out) == 0 {
		lines = nil
	}
	if len(lines) != len(paths) {
		return nil, fmt.Errorf("%s: %d lines for %d paths", f.bin, len(lines), len(paths))
	}
	for i, l := range lines {
		name, desc, ok := strings.Cut(l, "\x00: ")
		if !ok {
			return nil, fmt.Errorf("%s: cannot parse %q", f.bin, l)
		}
		if name != filepath.Join(root, paths[i]) {
			return nil, fmt.Errorf("%s: line %d is for %q, expected %q", f.bin, i, name, paths[i])
		}
		if strings.HasPrefix(desc, "cannot open `") || strings.HasPrefix(desc, "ERROR: ") {
			desc = "error: " + strings.TrimPrefix(desc, "ERROR: ")
		}
		lines[i] = desc
	}
	return lines, nil
}
