package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lczyk/assert"

	chiselfile "github.com/lczyk/chisel-file"
	"github.com/lczyk/chisel-file/internal/listing"
	"github.com/lczyk/chisel-file/internal/magic"
)

func TestParseArgs(t *testing.T) {
	f, pkgs, err := parseArgs([]string{"--release", "/r", "--arch", "amd64,arm64", "--arch=s390x", "-v",
		"bash", "--slice", "bins", "--ignore", "/usr/share/doc/**,/usr/share/man/**", "--json",
		"--raw", "--keep", "--no-group", "--backend", "builtin", "coreutils"}, io.Discard)
	assert.NoError(t, err)
	assert.EqualArrays(t, pkgs, []string{"bash", "coreutils"})
	assert.Equal(t, f.Release, "/r")
	assert.EqualArrays(t, f.Arches, []string{"amd64", "arm64", "s390x"})
	assert.EqualArrays(t, f.Slices, []string{"bins"})
	assert.EqualArrays(t, f.Ignores, []string{"/usr/share/doc/**", "/usr/share/man/**"})
	assert.That(t, f.JSON && f.Raw && f.Keep && f.NoGroup)
	assert.Equal(t, f.Backend, "builtin")
	assert.Len(t, f.Verbose, 1)

	f, _, err = parseArgs([]string{"bash"}, io.Discard)
	assert.NoError(t, err)
	assert.Equal(t, f.Release, ".")
	assert.Equal(t, f.Backend, "")
	assert.Equal(t, f.Jobs, -1)

	_, _, err = parseArgs([]string{"--backend", "magic", "bash"}, io.Discard)
	assert.Error(t, err, "--backend must be builtin or path")
	_, _, err = parseArgs([]string{"--nope", "bash"}, io.Discard)
	assert.Error(t, err, "unknown flag `nope'")

	var help bytes.Buffer
	f, _, err = parseArgs([]string{"-h"}, &help)
	assert.NoError(t, err)
	assert.That(t, f.help)
	assert.ContainsString(t, help.String(), "--backend=<builtin|path>")
}

func TestSelectSlices(t *testing.T) {
	deb, only := selectSlices(nil)
	assert.That(t, deb)
	assert.That(t, only != nil && len(only) == 0)
	deb, only = selectSlices([]string{"all"})
	assert.That(t, !deb)
	assert.That(t, only == nil)
	deb, only = selectSlices([]string{"bins", "deb", "bash_config"})
	assert.That(t, deb)
	assert.EqualArrays(t, only, []string{"bins", "bash_config"})
	deb, only = selectSlices([]string{"bins", "all"})
	assert.That(t, !deb)
	assert.That(t, only == nil)
}

func sample() []listing.Package {
	entry := func(path, line string) magic.Entry { return magic.Entry{Path: path, Line: line} }
	arch := func(name, triplet, machine, loader string) listing.Arch {
		return listing.Arch{Arch: name, Available: true, Version: "3.0.13-1", DebArch: name, Slices: map[string][]magic.Entry{
			"bins": {
				entry("/usr/bin/openssl", "ELF 64-bit LSB pie executable, "+machine+", version 1 (SYSV), dynamically linked, interpreter "+loader+", BuildID[sha1]=ab, for GNU/Linux 3.2.0, stripped"),
				entry("/usr/bin/c_rehash", "Perl script text executable"),
				entry("/usr/bin/ssl", "symbolic link to openssl"),
			},
			"libs": {entry("/usr/lib/"+triplet+"/libssl.so.3", "ELF 64-bit LSB shared object, "+machine+", version 1 (SYSV), dynamically linked, BuildID[sha1]=cd, stripped")},
		}}
	}
	return []listing.Package{
		{Name: "openssl", Slices: []string{"bins", "libs"}, Arches: []listing.Arch{
			arch("amd64", "x86_64-linux-gnu", "x86-64", "/lib64/ld-linux-x86-64.so.2"),
			{Arch: "i386"},
			arch("arm64", "aarch64-linux-gnu", "ARM aarch64", "/lib/ld-linux-aarch64.so.1"),
			{Arch: "s390x", Err: errors.New("chisel cut ... exited 1\nboom")},
		}},
		{Name: "tzdata", Slices: []string{"deb", "zones"}, Arches: []listing.Arch{{
			Arch: "amd64", Available: true, Version: "2026a-1", DebArch: "all",
			Slices: map[string][]magic.Entry{"deb": {entry("/usr/share/zoneinfo/UTC", "timezone data")}, "zones": {entry("/usr/share/zoneinfo/UTC", "timezone data")}},
		}}},
	}
}

func TestRenderText(t *testing.T) {
	var out bytes.Buffer
	renderText(&out, sample(), true, style{})
	assert.Equal(t, out.String(), `openssl 3.0.13-1 bins [amd64, arm64]
/usr/bin/c_rehash: Perl script, text executable
/usr/bin/openssl: ELF <arch> pie, dynamic, <ld>, stripped
/usr/bin/ssl -> openssl
openssl 3.0.13-1 libs [amd64, arm64]
/usr/lib/<arch>-linux-<gnu>/libssl.so.3: ELF <arch> lib, dynamic, stripped
openssl [i386]: not available
openssl [s390x]: error
  chisel cut ... exited 1
  boom
tzdata 2026a-1 (all) deb [amd64]
/usr/share/zoneinfo/UTC: timezone data
tzdata 2026a-1 (all) zones [amd64]
/usr/share/zoneinfo/UTC: timezone data
`)
}

func TestRenderTextUngrouped(t *testing.T) {
	var out bytes.Buffer
	renderText(&out, sample()[:1], false, style{})
	assert.ContainsString(t, out.String(), "openssl 3.0.13-1 bins [amd64]\n/usr/bin/c_rehash: Perl script text executable\n/usr/bin/openssl: ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=ab, for GNU/Linux 3.2.0, stripped\n")
	assert.ContainsString(t, out.String(), "openssl 3.0.13-1 libs [arm64]\n/usr/lib/aarch64-linux-gnu/libssl.so.3: ELF 64-bit LSB shared object, ARM aarch64")
	assert.ContainsString(t, out.String(), "openssl [i386]: not available\n")
}

func TestRenderTextColoured(t *testing.T) {
	var out bytes.Buffer
	renderText(&out, sample()[:1], true, colours)
	assert.ContainsString(t, out.String(), "\x1b[1;34mopenssl\x1b[0m 3.0.13-1 bins \x1b[2m[amd64, arm64]\x1b[0m\n")
	assert.ContainsString(t, out.String(), "\x1b[33mnot available\x1b[0m")
	assert.ContainsString(t, out.String(), "\x1b[31merror\x1b[0m")
}

func TestStyleFor(t *testing.T) {
	assert.Equal(t, styleFor(&bytes.Buffer{}), style{})
	t.Setenv("NO_COLOR", "1")
	assert.Equal(t, styleFor(os.Stdout), style{})
}

// Without grouping every record is one architecture's own path and line.
func TestRenderJSONUngrouped(t *testing.T) {
	var out bytes.Buffer
	run := runInfo{Backend: "builtin", Magic: "softmagic (file 5.48)", Chisel: "v1", Release: "ubuntu-24.04"}
	assert.NoError(t, renderJSON(&out, run, sample()[:1], false, false))
	assert.ContainsString(t, out.String(), `"path": "/usr/lib/x86_64-linux-gnu/libssl.so.3"`)
	assert.ContainsString(t, out.String(), `"path": "/usr/lib/aarch64-linux-gnu/libssl.so.3"`)
	assert.That(t, !strings.Contains(out.String(), "<arch>"))
	assert.ContainsString(t, out.String(), "ELF 64-bit LSB shared object, ARM aarch64")
}

func TestRenderJSON(t *testing.T) {
	var out bytes.Buffer
	run := runInfo{Backend: "path", Magic: "file-5.48", Chisel: "v1", Release: "ubuntu-24.04"}
	assert.NoError(t, renderJSON(&out, run, sample(), true, true))
	assert.ContainsString(t, out.String(), `"path": "/usr/lib/<arch>-linux-<gnu>/libssl.so.3"`)
	var got struct {
		Run      runInfo `json:"run"`
		Packages []struct {
			Name   string `json:"name"`
			Arches []struct {
				Arch      string  `json:"arch"`
				Available bool    `json:"available"`
				Version   *string `json:"version"`
				Error     *string `json:"error"`
			} `json:"arches"`
			Slices []struct {
				Name  string `json:"name"`
				Files []struct {
					Path   string            `json:"path"`
					Arches []string          `json:"arches"`
					File   string            `json:"file"`
					Raw    map[string]string `json:"raw"`
				} `json:"files"`
			} `json:"slices"`
		} `json:"packages"`
	}
	assert.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, got.Run.Magic, "file-5.48")
	assert.Len(t, got.Packages, 2)
	p := got.Packages[0]
	assert.Len(t, p.Arches, 4)
	assert.That(t, !p.Arches[1].Available && p.Arches[1].Version == nil)
	assert.ContainsString(t, *p.Arches[3].Error, "boom")
	assert.Len(t, p.Slices, 2)
	assert.Equal(t, p.Slices[0].Name, "bins")
	assert.Len(t, p.Slices[0].Files, 3)
	openssl := p.Slices[0].Files[1]
	assert.Equal(t, p.Slices[0].Files[2].File, "symbolic link to openssl")
	assert.Equal(t, openssl.Path, "/usr/bin/openssl")
	assert.EqualArrays(t, openssl.Arches, []string{"amd64", "arm64"})
	assert.Equal(t, openssl.File, "ELF <arch> pie, dynamic, <ld>, stripped")
	assert.Len(t, openssl.Raw, 2)
	assert.ContainsString(t, openssl.Raw["arm64"], "ARM aarch64")

	out.Reset()
	assert.NoError(t, renderJSON(&out, run, sample(), false, true))
	assert.That(t, !bytes.Contains(out.Bytes(), []byte(`"raw"`)))
}

const fakeChisel = `#!/bin/sh
while [ $# -gt 1 ]; do
  case "$1" in --root) root="$2"; shift 2;; *) shift;; esac
done
if [ "$1" = "$FAKE_FAIL" ]; then echo boom >&2; exit 1; fi
if [ -d "$FAKE_TREES/$1" ]; then cp -R "$FAKE_TREES/$1/." "$root/"; fi
`

func fixture(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	rel := filepath.Join(tmp, "release")
	assert.NoError(t, os.MkdirAll(filepath.Join(rel, "slices"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(rel, "chisel.yaml"), []byte("format: v1\n"), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(rel, "slices", "demo.yaml"),
		[]byte("package: demo\nslices:\n  bins:\n    contents:\n      /usr/bin/demo:\n"), 0644))
	for p, content := range map[string]string{
		"demo_file-all/usr/bin/demo":                   "#!/bin/sh\n",
		"demo_file-all/usr/share/doc/demo/README":      "hello\n",
		"demo_bins/usr/bin/demo":                       "#!/bin/sh\n",
		"demo_file-ignore-0/usr/share/doc/demo/README": "hello\n",
	} {
		assert.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(tmp, "trees", p)), 0755))
		assert.NoError(t, os.WriteFile(filepath.Join(tmp, "trees", p), []byte(content), 0755))
	}
	bin := filepath.Join(tmp, "chisel")
	assert.NoError(t, os.WriteFile(bin, []byte(fakeChisel), 0755))
	t.Setenv("CHISEL", bin)
	t.Setenv("FAKE_TREES", filepath.Join(tmp, "trees"))
	t.Setenv("FAKE_FAIL", "")
	// Main views the real chisel cache otherwise.
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	return rel
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code := fn()
	os.Stdout = orig
	_ = w.Close()
	return code, <-done
}

func TestMainExitCodes(t *testing.T) {
	rel := fixture(t)
	base := []string{"--release", rel, "--arch", "amd64", "--backend", "builtin"}

	// The deb alone by default.
	code, out := captureStdout(t, func() int { return Main(append(base, "--ignore", "/usr/share/**", "demo")) })
	assert.Equal(t, code, 0)
	assert.Equal(t, out, "demo deb [amd64]\n/usr/bin/demo: POSIX shell script, text executable\n")

	code, out = captureStdout(t, func() int { return Main(append(base, "--slice", "all", "demo")) })
	assert.Equal(t, code, 0)
	assert.Equal(t, out, "demo bins [amd64]\n/usr/bin/demo: POSIX shell script, text executable\n")

	code, out = captureStdout(t, func() int { return Main(append(base, "--slice", "deb,bins", "--ignore", "/usr/share/**", "demo")) })
	assert.Equal(t, code, 0)
	assert.Equal(t, out, "demo deb [amd64]\n/usr/bin/demo: POSIX shell script, text executable\ndemo bins [amd64]\n/usr/bin/demo: POSIX shell script, text executable\n")

	t.Setenv("FAKE_FAIL", "demo_bins")
	code, out = captureStdout(t, func() int { return Main(append(base, "--slice", "all", "demo")) })
	assert.Equal(t, code, 2)
	assert.ContainsString(t, out, "demo [amd64]: error\n")
	assert.ContainsString(t, out, "boom")
	t.Setenv("FAKE_FAIL", "")

	code, out = captureStdout(t, func() int { return Main(append(base, "--json", "demo")) })
	assert.Equal(t, code, 0)
	assert.ContainsString(t, out, `"backend": "builtin"`)
	assert.ContainsString(t, out, `"arch": "amd64"`)

	t.Setenv("FILE", "/nope/file")
	code, _ = captureStdout(t, func() int { return Main([]string{"--release", rel, "--arch", "amd64", "--backend", "path", "demo"}) })
	assert.Equal(t, code, 2)
	code, out = captureStdout(t, func() int { return Main([]string{"--release", rel, "--arch", "amd64", "demo"}) })
	assert.Equal(t, code, 0)
	assert.ContainsString(t, out, "POSIX shell script")

	for _, args := range [][]string{
		nil,
		{"--release", rel},
		{"--release", rel, "--arch", "x86", "demo"},
		{"--release", rel, "--bogus", "demo"},
		{"--release", t.TempDir(), "demo"},
		{"--release", rel, "--slice", "nope", "demo"},
	} {
		code, _ := captureStdout(t, func() int { return Main(args) })
		assert.Equal(t, code, 2, args)
	}
	// A package with no package slices file has no slices, and --slice
	// all finds nothing of it (the fake chisel here has every package).
	code, out = captureStdout(t, func() int {
		return Main([]string{"--release", rel, "--arch", "amd64", "--backend", "builtin", "--slice", "all", "nosuch"})
	})
	assert.Equal(t, code, 0)
	assert.Equal(t, out, "nosuch [amd64]: no slices\n")

	version := fileVersion(chiselfile.VersionFile)
	assert.That(t, version != "")
	code, out = captureStdout(t, func() int { return Main([]string{"version"}) })
	assert.Equal(t, code, 0)
	assert.ContainsString(t, out, version)
	for _, args := range [][]string{{"help"}, {"--help"}, {"demo", "--help"}} {
		code, out = captureStdout(t, func() int { return Main(args) })
		assert.Equal(t, code, 0, args)
		assert.ContainsString(t, out, "Usage:", args)
	}
}

func TestFileVersion(t *testing.T) {
	assert.Equal(t, fileVersion("# c\n\n1.2.3\n"), "1.2.3")
	assert.Equal(t, fileVersion("# c\n"), "")
}
