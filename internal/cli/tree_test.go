package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/lczyk/assert"

	"github.com/lczyk/chisel-file/internal/listing"
	"github.com/lczyk/chisel-file/internal/magic"
)

// boxes spells the tree glyphs in ascii in a test's expected text: "T- ",
// "L- " and "|  " for the three the tree draws.
func boxes(s string) string {
	return strings.NewReplacer("T- ", "\u251c\u2500 ", "L- ", "\u2514\u2500 ", "|  ", "\u2502  ").Replace(s)
}

func TestRenderTree(t *testing.T) {
	var out bytes.Buffer
	renderText(&out, sample(), true, true, style{})
	assert.Equal(t, out.String(), boxes(`openssl 3.0.13-1 bins [amd64, arm64]
/usr/bin/
T- c_rehash: Perl script, text executable
T- openssl: ELF <arch> pie, dynamic, <ld>, stripped
L- ssl -> openssl
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
`))

	// The glyphs are dim, the rest as the flat report colours it.
	out.Reset()
	renderText(&out, sample()[:1], true, true, colours)
	assert.ContainsString(t, out.String(), "\x1b[1;34mopenssl\x1b[0m 3.0.13-1 bins \x1b[2m[amd64, arm64]\x1b[0m\n/usr/bin/\n"+
		boxes("\x1b[2mT- \x1b[0mc_rehash: Perl script, text executable\n"))
}

// libssl is a package whose architectures part on two of its files: a
// block of what they share, and a block of each one's own.
func libssl() []listing.Package {
	lib := func(machine, stripped string) string {
		return "ELF 64-bit LSB shared object, " + machine + ", version 1 (SYSV), dynamically linked, BuildID[sha1]=cd, " + stripped
	}
	arch := func(name, triplet, machine, engines string) listing.Arch {
		dir := "/usr/lib/" + triplet + "/"
		return listing.Arch{Arch: name, Available: true, Version: "3.0.13-1", DebArch: name, Slices: map[string][]magic.Entry{"libs": {
			{Path: dir + "libssl.so.3", Line: lib(machine, "stripped")},
			{Path: dir + "libcrypto.so.3", Line: lib(machine, "stripped")},
			{Path: dir + "engines-3/padlock.so", Line: lib(machine, engines)},
			{Path: dir + "engines-3/afalg.so", Line: lib(machine, engines)},
		}}}
	}
	return []listing.Package{{Name: "libssl3t64", Slices: []string{"libs"}, Arches: []listing.Arch{
		arch("amd64", "x86_64-linux-gnu", "x86-64", "stripped"),
		arch("arm64", "aarch64-linux-gnu", "ARM aarch64", "with debug_info, not stripped"),
	}}}
}

// Architectures fold first and each block is drawn as a tree of its own
// files: the directories over a file a block does not share are drawn to
// hold it, with the architecture's own path in a block of one.
func TestRenderTreeFolded(t *testing.T) {
	var out bytes.Buffer
	renderText(&out, libssl(), true, true, style{})
	assert.Equal(t, out.String(), boxes(`libssl3t64 3.0.13-1 libs [amd64, arm64]
/usr/lib/<arch>-linux-<gnu>/
T- libcrypto.so.3: ELF <arch> lib, dynamic, stripped
L- libssl.so.3: ELF <arch> lib, dynamic, stripped
libssl3t64 3.0.13-1 libs [amd64]
/usr/lib/x86_64-linux-gnu/engines-3/
T- afalg.so: ELF <arch> lib, dynamic, stripped
L- padlock.so: ELF <arch> lib, dynamic, stripped
libssl3t64 3.0.13-1 libs [arm64]
/usr/lib/aarch64-linux-gnu/engines-3/
T- afalg.so: ELF <arch> lib, dynamic, with debug_info, not stripped
L- padlock.so: ELF <arch> lib, dynamic, with debug_info, not stripped
`))

	// With every architecture a block of its own, one tree each, raw.
	out.Reset()
	renderText(&out, libssl(), false, true, style{})
	assert.ContainsString(t, out.String(), boxes(`libssl3t64 3.0.13-1 libs [amd64]
/usr/lib/x86_64-linux-gnu/
T- engines-3/
|  T- afalg.so: ELF 64-bit LSB shared object, x86-64,`))
	assert.ContainsString(t, out.String(), boxes(`L- libssl.so.3: ELF 64-bit LSB shared object, x86-64,`))
	assert.ContainsString(t, out.String(), "libssl3t64 3.0.13-1 libs [arm64]\n/usr/lib/aarch64-linux-gnu/\n")
}

// The tree holds the flat report's files, one line each, and adds only the
// directories that hold them.
func TestRenderTreeOneLineEach(t *testing.T) {
	for _, group := range []bool{true, false} {
		var flat, tree bytes.Buffer
		renderText(&flat, libssl(), group, false, style{})
		renderText(&tree, libssl(), group, true, style{})
		holders := 0
		for _, l := range strings.Split(strings.TrimSuffix(tree.String(), "\n"), "\n") {
			if strings.HasSuffix(l, "/") {
				holders++
			}
		}
		assert.Equal(t, strings.Count(tree.String(), "\n")-holders, strings.Count(flat.String(), "\n"), group)
	}
}

func TestParseArgsTree(t *testing.T) {
	f, _, err := parseArgs([]string{"-T", "bash"}, io.Discard)
	assert.NoError(t, err)
	assert.That(t, f.Tree)
	_, _, err = parseArgs([]string{"--tree", "--json", "bash"}, io.Discard)
	assert.Error(t, err, "--tree draws the text report, so it does not go with --json")
}
