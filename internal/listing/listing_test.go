package listing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lczyk/assert"
	"github.com/lczyk/chisel-coverage/pkg/cut"
	"github.com/lczyk/chisel-coverage/pkg/cuts"

	"github.com/lczyk/chisel-file/internal/magic"
)

// fakeChisel fills --root from $FAKE_TREES/<arch>/<slice>/. A package with no
// <arch>/<pkg>_file-all tree is not available on that arch; a slice listed in
// $FAKE_FAIL fails; a slice listed in $FAKE_NOCONTENT fails the way chisel
// fails a glob matching nothing; a <arch>/<pkg>.version file is logged the way
// chisel logs a fetch, and a <arch>/<pkg>.all marker makes the fetched deb an
// arch: all one.
const fakeChisel = `#!/bin/sh
while [ $# -gt 1 ]; do
  case "$1" in
    --root) root="$2"; shift 2;;
    --arch) arch="$2"; shift 2;;
    *) shift;;
  esac
done
slice="$1"
pkg="${slice%%_*}"
[ -n "$FAKE_LOG" ] && printf '%s %s\n' "$arch" "$slice" >> "$FAKE_LOG"
case " $FAKE_FAIL " in *" $slice "*) echo "boom" >&2; exit 1;; esac
case " $FAKE_NOCONTENT " in *" $slice "*) echo "error: cannot extract from package \"$pkg\": no content at /nope/**" >&2; exit 1;; esac
if [ ! -d "$FAKE_TREES/$arch/${pkg}_file-all" ]; then
  echo "error: cannot find package \"$pkg\" in archive(s)" >&2
  exit 1
fi
if [ -f "$FAKE_TREES/$arch/$pkg.version" ]; then
  debarch=$arch
  [ -f "$FAKE_TREES/$arch/$pkg.all" ] && debarch=all
  echo "Fetching pool/main/x/$pkg/${pkg}_$(cat "$FAKE_TREES/$arch/$pkg.version")_$debarch.deb..." >&2
fi
tree="$FAKE_TREES/$arch/$slice"
if [ -d "$tree" ]; then cp -R "$tree/." "$root/"; fi
exit 0
`

// fakeFile answers "file -N -0 -f -" with each file's own content as its
// description, so a fixture says what file would say by writing it.
const fakeFile = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "file-9.99"; exit 0; fi
while read -r p; do
  printf '%s\000: %s\n' "$p" "$(cat "$p")"
done
`

const bashYAML = `package: bash

slices:
  config:
    contents:
      /etc/skel/.bashrc:
  bins:
    contents:
      /usr/bin/bash:
      /usr/bin/sh:
      /usr/lib/*-linux-*/bash/loadables.so:
  scripts:
    mutate: |
      pass
`

const dashYAML = `package: dash

slices:
  bins:
    contents:
      /usr/bin/dash:
`

type fixture struct {
	release string
	trees   string
	log     string
	opts    Options
}

func write(t *testing.T, p, content string) {
	t.Helper()
	assert.NoError(t, os.MkdirAll(filepath.Dir(p), 0755))
	assert.NoError(t, os.WriteFile(p, []byte(content), 0644))
}

func setup(t *testing.T) *fixture {
	t.Helper()
	tmp := t.TempDir()
	f := &fixture{
		release: filepath.Join(tmp, "release"),
		trees:   filepath.Join(tmp, "trees"),
		log:     filepath.Join(tmp, "log"),
	}
	write(t, filepath.Join(f.release, "chisel.yaml"), "format: v1\n")
	write(t, filepath.Join(f.release, "slices", "bash.yaml"), bashYAML)
	write(t, filepath.Join(f.release, "slices", "dash.yaml"), dashYAML)

	elf := map[string]string{
		"amd64": "ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=aa, for GNU/Linux 3.2.0, stripped",
		"arm64": "ELF 64-bit LSB pie executable, ARM aarch64, version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-aarch64.so.1, BuildID[sha1]=bb, for GNU/Linux 3.7.0, stripped",
	}
	for arch, triplet := range map[string]string{"amd64": "x86_64-linux-gnu", "arm64": "aarch64-linux-gnu"} {
		all := filepath.Join(f.trees, arch, "bash_file-all")
		write(t, filepath.Join(all, "usr/bin/bash"), elf[arch])
		write(t, filepath.Join(all, "usr/lib", triplet, "bash/loadables.so"), "ELF 64-bit LSB shared object, "+map[string]string{"amd64": "x86-64", "arm64": "ARM aarch64"}[arch]+", version 1 (SYSV), dynamically linked, stripped")
		write(t, filepath.Join(all, "usr/share/man/man1/bash.1.gz"), "gzip compressed data")
		write(t, filepath.Join(all, "etc/skel/.bashrc"), "ASCII text")
		assert.NoError(t, os.Symlink("bash", filepath.Join(all, "usr/bin/sh")))
		bins := filepath.Join(f.trees, arch, "bash_bins")
		write(t, filepath.Join(bins, "usr/bin/bash"), elf[arch])
		write(t, filepath.Join(bins, "usr/lib", triplet, "bash/loadables.so"), "ELF 64-bit LSB shared object, "+map[string]string{"amd64": "x86-64", "arm64": "ARM aarch64"}[arch]+", version 1 (SYSV), dynamically linked, stripped")
		assert.NoError(t, os.Symlink("bash", filepath.Join(bins, "usr/bin/sh")))
		write(t, filepath.Join(f.trees, arch, "bash_config/etc/skel/.bashrc"), "ASCII text")
		write(t, filepath.Join(f.trees, arch, "bash_file-ignore-0/usr/share/man/man1/bash.1.gz"), "gzip compressed data")
		write(t, filepath.Join(f.trees, arch, "bash.version"), "5.2-1")
		write(t, filepath.Join(f.trees, arch, "dash_file-all/usr/bin/dash"), "a script")
		write(t, filepath.Join(f.trees, arch, "dash_bins/usr/bin/dash"), "a script")
		write(t, filepath.Join(f.trees, arch, "dash.version"), "0.5")
	}
	// On arm64 the loadable is not stripped: a real difference.
	write(t, filepath.Join(f.trees, "arm64", "bash_bins/usr/lib/aarch64-linux-gnu/bash/loadables.so"), "ELF 64-bit LSB shared object, ARM aarch64, version 1 (SYSV), dynamically linked, with debug_info, not stripped")

	chisel := filepath.Join(tmp, "chisel")
	assert.NoError(t, os.WriteFile(chisel, []byte(fakeChisel), 0755))
	file := filepath.Join(tmp, "file")
	assert.NoError(t, os.WriteFile(file, []byte(fakeFile), 0755))
	t.Setenv("FILE", file)
	t.Setenv("FAKE_TREES", f.trees)
	t.Setenv("FAKE_LOG", f.log)
	t.Setenv("FAKE_FAIL", "")
	t.Setenv("FAKE_NOCONTENT", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "xdg"))
	backend, err := magic.NewPath()
	assert.NoError(t, err)
	f.opts = Options{Backend: backend, Cuts: cuts.Options{Release: f.release, Arches: []string{"amd64", "arm64", "i386"}, Runner: &cut.Runner{Bin: chisel}}}
	return f
}

func (f *fixture) cuts(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(f.log)
	assert.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func lines(entries []magic.Entry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Path+": "+e.Line)
	}
	return out
}

func TestRunLists(t *testing.T) {
	f := setup(t)
	f.opts.Cuts.Ignores = []string{"/usr/share/man/**"}
	f.opts.Cuts.Only = nil
	res, err := Run(context.Background(), f.opts, []string{"bash"})
	assert.NoError(t, err)
	assert.Len(t, res, 1)
	p := res[0]
	assert.Equal(t, p.Name, "bash")
	assert.EqualArrays(t, p.Slices, []string{"config", "bins", "scripts"})

	amd64 := p.Arches[0]
	assert.That(t, amd64.Available)
	assert.Equal(t, amd64.Version, "5.2-1")
	assert.Len(t, amd64.Slices, 3)
	assert.EqualArrays(t, lines(amd64.Slices["bins"]), []string{
		"/usr/bin/bash: ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=aa, for GNU/Linux 3.2.0, stripped",
		"/usr/bin/sh: symbolic link to bash",
		"/usr/lib/x86_64-linux-gnu/bash/loadables.so: ELF 64-bit LSB shared object, x86-64, version 1 (SYSV), dynamically linked, stripped",
	})
	assert.EqualArrays(t, lines(amd64.Slices["config"]), []string{"/etc/skel/.bashrc: ASCII text"})
	assert.EqualArrays(t, lines(amd64.Slices["scripts"]), []string{})
	_, whole := amd64.Slices[DebSlice]
	assert.That(t, !whole)
	assert.That(t, !p.Arches[2].Available)
	assert.Nil(t, p.Arches[2].Slices)
	// The probe leads, the whole deb is never cut, ignores are.
	cuts := f.cuts(t)
	assert.EqualArrays(t, cuts[:1], []string{"amd64 bash_file-probe"})
	assert.That(t, !strings.Contains(strings.Join(cuts, "\n"), "file-all"))
	assert.That(t, strings.Contains(strings.Join(cuts, "\n"), "amd64 bash_file-ignore-0"))

	groups := Groups(p)
	assert.Len(t, groups, 4)
	assert.EqualArrays(t, groups[0].Arches, []string{"amd64", "arm64"})
	assert.Equal(t, groups[0].Version, "5.2-1")
	assert.Len(t, groups[0].Slices, 3)
	assert.Equal(t, groups[0].Slices[0].Name, "config")
	assert.Equal(t, groups[0].Slices[1].Name, "bins")
	assert.Len(t, groups[0].Slices[1].Files, 2)
	bash := groups[0].Slices[1].Files[0]
	assert.Equal(t, bash.Path, "/usr/bin/bash")
	assert.Equal(t, bash.Line, "ELF <arch> pie, dynamic, <ld>, stripped")
	assert.Len(t, bash.Raw, 2)
	assert.ContainsString(t, bash.Raw[1], "ARM aarch64")
	assert.Equal(t, groups[0].Slices[1].Files[1].Path, "/usr/bin/sh")
	assert.Equal(t, groups[0].Slices[2].Name, "scripts")
	assert.Len(t, groups[0].Slices[2].Files, 0)
	// The loadable differs: one block each, real paths, normalised lines.
	assert.EqualArrays(t, groups[1].Arches, []string{"amd64"})
	assert.Len(t, groups[1].Slices, 1)
	assert.Equal(t, groups[1].Slices[0].Files[0].Path, "/usr/lib/x86_64-linux-gnu/bash/loadables.so")
	assert.Equal(t, groups[1].Slices[0].Files[0].Line, "ELF <arch> lib, dynamic, stripped")
	assert.EqualArrays(t, groups[2].Arches, []string{"arm64"})
	assert.Equal(t, groups[2].Slices[0].Files[0].Line, "ELF <arch> lib, dynamic, with debug_info, not stripped")
	assert.EqualArrays(t, groups[3].Arches, []string{"i386"})
	assert.That(t, !groups[3].Available)
	assert.Len(t, groups[3].Slices, 0)

	flat := Ungrouped(p)
	assert.Len(t, flat, 3)
	assert.Equal(t, flat[0].Slices[1].Files[0].Line, amd64.Slices["bins"][0].Line)
}

func TestRunDeb(t *testing.T) {
	f := setup(t)
	f.opts.Deb = true
	f.opts.Cuts.Arches = []string{"amd64"}
	f.opts.Cuts.Ignores = []string{"/usr/share/**"}
	write(t, filepath.Join(f.trees, "amd64", "zsh_file-all/usr/bin/zsh"), "a shell")
	res, err := Run(context.Background(), f.opts, []string{"bash", "zsh"})
	assert.NoError(t, err)
	assert.EqualArrays(t, res[0].Slices, []string{"deb", "config", "bins", "scripts"})
	assert.EqualArrays(t, lines(res[0].Arches[0].Slices["deb"]), []string{
		"/etc/skel/.bashrc: ASCII text",
		"/usr/bin/bash: ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=aa, for GNU/Linux 3.2.0, stripped",
		"/usr/bin/sh: symbolic link to bash",
		"/usr/lib/x86_64-linux-gnu/bash/loadables.so: ELF 64-bit LSB shared object, x86-64, version 1 (SYSV), dynamically linked, stripped",
	})
	// zsh has no package slices file: the deb alone.
	assert.EqualArrays(t, res[1].Slices, []string{"deb"})
	assert.That(t, res[1].Arches[0].Available)
	assert.EqualArrays(t, lines(res[1].Arches[0].Slices["deb"]), []string{"/usr/bin/zsh: a shell"})
	assert.Equal(t, f.cuts(t)[0], "amd64 bash_file-all")

	// The deb alone, which is the default.
	assert.NoError(t, os.Remove(f.log))
	f.opts.Cuts.Only = []string{}
	res, err = Run(context.Background(), f.opts, []string{"bash"})
	assert.NoError(t, err)
	assert.EqualArrays(t, res[0].Slices, []string{"deb"})
	assert.That(t, !strings.Contains(strings.Join(f.cuts(t), "\n"), "bash_bins"))

	// A real slice called deb is refused when the deb is listed.
	write(t, filepath.Join(f.release, "slices", "zsh.yaml"), "package: zsh\nslices:\n  deb:\n    contents:\n      /usr/bin/zsh:\n")
	_, err = Run(context.Background(), f.opts, []string{"zsh"})
	assert.Error(t, err, `zsh has a slice named "deb"`)
	// Otherwise it is a slice like any other.
	f.opts.Deb = false
	f.opts.Cuts.Only = nil
	write(t, filepath.Join(f.trees, "amd64", "zsh_deb/usr/bin/zsh"), "a shell")
	res, err = Run(context.Background(), f.opts, []string{"zsh"})
	assert.NoError(t, err)
	assert.EqualArrays(t, res[0].Slices, []string{"deb"})
	assert.EqualArrays(t, lines(res[0].Arches[0].Slices["deb"]), []string{"/usr/bin/zsh: a shell"})
}

// A package with no package slices file has no slices: --slice all
// lists nothing of it, a bare slice name is not its concern, a prefixed
// one is.
func TestRunNoPackageSlicesFileHasNoSlices(t *testing.T) {
	f := setup(t)
	f.opts.Cuts.Arches = []string{"amd64"}
	write(t, filepath.Join(f.trees, "amd64", "zsh_file-all/usr/bin/zsh"), "a shell")
	res, err := Run(context.Background(), f.opts, []string{"bash", "zsh"})
	assert.NoError(t, err)
	assert.EqualArrays(t, res[1].Slices, []string{})
	assert.That(t, res[1].Arches[0].Available)
	assert.Equal(t, res[1].Arches[0].Version, "")
	assert.Len(t, Groups(res[1]), 1)
	assert.Len(t, Groups(res[1])[0].Slices, 0)

	f.opts.Cuts.Only = []string{"bins"}
	res, err = Run(context.Background(), f.opts, []string{"bash", "zsh"})
	assert.NoError(t, err)
	assert.EqualArrays(t, res[0].Slices, []string{"bins"})
	assert.EqualArrays(t, res[1].Slices, []string{})

	f.opts.Cuts.Only = []string{"zsh_bins"}
	_, err = Run(context.Background(), f.opts, []string{"bash", "zsh"})
	assert.Error(t, err, "zsh has no slice")
}

func TestRunArchAllStandsIn(t *testing.T) {
	f := setup(t)
	for _, arch := range []string{"amd64", "arm64"} {
		write(t, filepath.Join(f.trees, arch, "dash.all"), "")
	}
	res, err := Run(context.Background(), f.opts, []string{"dash"})
	assert.NoError(t, err)
	d := res[0]
	assert.Equal(t, d.Arches[0].DebArch, "all")
	assert.Equal(t, d.Arches[1].StandIn, "amd64")
	assert.EqualArrays(t, lines(d.Arches[1].Slices["bins"]), []string{"/usr/bin/dash: a script"})
	assert.That(t, !strings.Contains(strings.Join(f.cuts(t), "\n"), "arm64 dash_bins"))
	groups := Groups(d)
	assert.Len(t, groups, 2)
	assert.EqualArrays(t, groups[0].Arches, []string{"amd64", "arm64"})
	assert.Equal(t, groups[0].DebArch, "all")
	assert.Equal(t, groups[0].Slices[0].Files[0].Path, "/usr/bin/dash")
}

func TestRunCutFailureIsPerArch(t *testing.T) {
	f := setup(t)
	t.Setenv("FAKE_FAIL", "bash_config")
	res, err := Run(context.Background(), f.opts, []string{"bash"})
	assert.NoError(t, err)
	assert.Error(t, res[0].Arches[0].Err, "boom")
	assert.Nil(t, res[0].Arches[0].Slices)
	groups := Groups(res[0])
	assert.Len(t, groups, 3)
	assert.Error(t, groups[0].Err, "boom")
	assert.Len(t, groups[0].Slices, 0)
}

func TestRunBackendFailureFailsTheRun(t *testing.T) {
	f := setup(t)
	f.opts.Backend = broken{}
	_, err := Run(context.Background(), f.opts, []string{"bash"})
	assert.Error(t, err, "bash_config: no magic today")
}

type broken struct{}

func (broken) Name() string    { return "broken" }
func (broken) Version() string { return "" }
func (broken) Identify(string, []string) ([]string, error) {
	return nil, errors.New("no magic today")
}

// A slice with nothing on one architecture and files on the others is a
// block of its own for that architecture, not silently theirs too.
func TestGroupsEmptyOnOneArch(t *testing.T) {
	entry := func(path, line string) magic.Entry { return magic.Entry{Path: path, Line: line} }
	arch := func(name string) Arch {
		return Arch{Arch: name, Available: true, Version: "1", Slices: map[string][]magic.Entry{
			"bins": {entry("/usr/bin/x", "data")},
			"libs": {entry("/usr/lib/x", "data")},
		}}
	}
	amd64, arm64, s390x := arch("amd64"), arch("arm64"), arch("s390x")
	arm64.Slices["libs"] = nil
	s390x.Slices["libs"] = nil
	g := Groups(Package{Slices: []string{"bins", "libs"}, Arches: []Arch{amd64, arm64, s390x}})
	assert.Len(t, g, 3)
	assert.EqualArrays(t, g[0].Arches, []string{"amd64", "arm64", "s390x"})
	assert.Len(t, g[0].Slices, 1)
	assert.Equal(t, g[0].Slices[0].Name, "bins")
	assert.EqualArrays(t, g[1].Arches, []string{"arm64", "s390x"})
	assert.Len(t, g[1].Slices, 1)
	assert.Equal(t, g[1].Slices[0].Name, "libs")
	assert.Len(t, g[1].Slices[0].Files, 0)
	assert.EqualArrays(t, g[2].Arches, []string{"amd64"})
	assert.Equal(t, g[2].Slices[0].Files[0].Path, "/usr/lib/x")
}

func TestGroupsMergeSlicesOfTheSameArches(t *testing.T) {
	entry := func(path, line string) magic.Entry { return magic.Entry{Path: path, Line: line} }
	arch := func(name string) Arch {
		return Arch{Arch: name, Available: true, Version: "1", Slices: map[string][]magic.Entry{
			"bins": {entry("/usr/bin/x", "data")},
			"libs": {entry("/usr/lib/x", "data")},
		}}
	}
	amd64, arm64, i386 := arch("amd64"), arch("arm64"), arch("i386")
	i386.Slices["libs"] = append(i386.Slices["libs"], entry("/usr/lib/y", "data"))
	armhf := Arch{Arch: "armhf", Available: true, Version: "2", Slices: map[string][]magic.Entry{"bins": {}, "libs": {}}}
	g := Groups(Package{Slices: []string{"bins", "libs"}, Arches: []Arch{amd64, arm64, i386, armhf}})
	assert.Len(t, g, 3)
	assert.EqualArrays(t, g[0].Arches, []string{"amd64", "arm64", "i386"})
	assert.Len(t, g[0].Slices, 2)
	assert.Equal(t, g[0].Slices[0].Name, "bins")
	assert.Equal(t, g[0].Slices[1].Name, "libs")
	assert.Len(t, g[0].Slices[1].Files, 1)
	assert.EqualArrays(t, g[1].Arches, []string{"i386"})
	assert.Len(t, g[1].Slices, 1)
	assert.Equal(t, g[1].Slices[0].Name, "libs")
	assert.Equal(t, g[1].Slices[0].Files[0].Path, "/usr/lib/y")
	assert.EqualArrays(t, g[2].Arches, []string{"armhf"})
	assert.Equal(t, g[2].Version, "2")
	assert.Len(t, g[2].Slices, 2)
}
