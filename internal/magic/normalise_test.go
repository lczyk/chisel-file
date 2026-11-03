package magic

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/lczyk/assert"
	"github.com/lczyk/chisel-coverage/pkg/fold"
)

func TestNormalise(t *testing.T) {
	for _, tc := range []struct{ line, arch, want string }{
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=8a71473f5a4f0730928c71d7f218498db2ac782b, for GNU/Linux 3.2.0, stripped", "amd64",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 32-bit LSB pie executable, ARM, EABI5 version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-armhf.so.3, BuildID[sha1]=0d77dbd4379bf0b5af3e05be7fb6af1e4cea7c96, for GNU/Linux 3.2.0, stripped", "armhf",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit LSB pie executable, 64-bit PowerPC or cisco 7500, OpenPOWER ELF V2 ABI, version 1 (SYSV), dynamically linked, interpreter /lib64/ld64.so.2, BuildID[sha1]=ab, for GNU/Linux 3.10.0, stripped", "ppc64el",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit LSB pie executable, UCB RISC-V, RVC, double-float ABI, version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-riscv64-lp64d.so.1, BuildID[sha1]=ab, for GNU/Linux 4.15.0, stripped", "riscv64",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit MSB shared object, IBM S/390, version 1 (GNU/Linux), dynamically linked, BuildID[sha1]=ab, stripped", "s390x",
			"ELF <arch> lib, dynamic, stripped"},
		{"ELF 32-bit LSB shared object, ARM, EABI5 version 1 (SYSV), dynamically linked, BuildID[sha1]=ab, with debug_info, not stripped", "armhf",
			"ELF <arch> lib, dynamic, with debug_info, not stripped"},
		{"ELF 64-bit LSB shared object, x86-64, version 1 (GNU/Linux), static-pie linked, BuildID[sha1]=ab, stripped", "amd64",
			"ELF <arch> lib, static-pie, stripped"},
		// Another architecture's binary is loud.
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, stripped", "arm64",
			"ELF <other:amd64> pie, dynamic, /lib64/ld-linux-x86-64.so.2, stripped"},
		// An unknown machine or loader stays as it is.
		{"ELF 32-bit MSB executable, MIPS, MIPS32 version 1 (SYSV), statically linked, stripped", "amd64",
			"ELF 32-bit MSB exec, MIPS, MIPS32, static, stripped"},
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /usr/lib/klibc/bin/klibc.so, stripped", "amd64",
			"ELF <arch> pie, dynamic, /usr/lib/klibc/bin/klibc.so, stripped"},
		{"setuid ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, stripped", "amd64",
			"setuid ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit LSB relocatable, x86-64, version 1 (SYSV), not stripped", "amd64", "ELF <arch> obj, not stripped"},
		// Text encodings are noise, the kind is not.
		{"Python script, ASCII text executable", "amd64", "Python script, text executable"},
		{"Python script, Unicode text, UTF-8 text executable", "amd64", "Python script, text executable"},
		{"Perl script text executable", "amd64", "Perl script text executable"},
		{"ASCII text", "amd64", "text"},
		{"Unicode text, UTF-8 text", "amd64", "text"},
		{"Unicode text, UTF-8 text, with very long lines", "amd64", "text, with very long lines"},
		{"C source, ASCII text", "amd64", "C source, text"},
		{"ASCII text, with CRLF line terminators", "amd64", "text, with CRLF line terminators"},
		{"POSIX shell script, ASCII text executable", "amd64", "POSIX shell script, text executable"},
		{"empty", "amd64", "empty"},
		{"symbolic link to libz.so.1.3", "amd64", "symbolic link to libz.so.1.3"},
		{"symbolic link to x86_64-linux-gnu-ld.bfd", "amd64", "symbolic link to <arch>-linux-<gnu>-ld.bfd"},
		{"symbolic link to ../lib/aarch64-linux-gnu/ld-linux-aarch64.so.1", "arm64", "symbolic link to ../lib/<arch>-linux-<gnu>/ld-linux-aarch64.so.1"},
		{"symbolic link to /usr/lib/jvm/java-21-openjdk-amd64/bin/java", "amd64", "symbolic link to /usr/lib/jvm/java-21-openjdk-<arch>/bin/java"},
		// An arch: all deb is one deb everywhere: nothing of it names the architecture.
		{"symbolic link to x86_64-linux-gnu-ld.bfd", "", "symbolic link to x86_64-linux-gnu-ld.bfd"},
		{"gzip compressed data, max compression, from Unix, original size modulo 2^32 4660", "amd64", "gzip compressed data, max compression, from Unix"},
		{"gzip compressed data, was \"bash.1\", last modified: Thu Mar 14 2024, from Unix, original size modulo 2^32 4660", "amd64", "gzip compressed data, was \"bash.1\", from Unix"},
		{"current ar archive", "amd64", "current ar archive"},
		{"data", "amd64", "data"},
	} {
		assert.Equal(t, Normalise(tc.line, tc.arch), tc.want, tc.line)
	}
}

// TestNormaliseCorpus runs the table over real file 5.48 lines from every
// architecture: every path that exists on more than one architecture gets
// one line, except the paths listed as really different.
func TestNormaliseCorpus(t *testing.T) {
	f, err := os.Open("testdata/corpus.txt")
	assert.NoError(t, err)
	defer f.Close()
	lines := map[string]map[string][]string{} // normalised path -> normalised line -> arches
	n := 0
	for s := bufio.NewScanner(f); s.Scan(); {
		arch, rest, ok := strings.Cut(s.Text(), " ")
		assert.That(t, ok, s.Text())
		path, line, ok := strings.Cut(rest, ": ")
		assert.That(t, ok, s.Text())
		norm := Normalise(line, arch)
		assert.That(t, !strings.Contains(norm, "BuildID"), norm)
		assert.That(t, !strings.Contains(norm, "<other:"), norm)
		if strings.HasPrefix(norm, "ELF") {
			assert.That(t, strings.HasPrefix(norm, "ELF <arch> "), norm)
		}
		key := fold.Normalise(path, arch)
		if lines[key] == nil {
			lines[key] = map[string][]string{}
		}
		lines[key][norm] = append(lines[key][norm], arch)
		n++
	}
	assert.That(t, n > 400)
	// What the deb really ships differently per architecture.
	really := map[string]bool{
		"/usr/lib/<arch>-linux-<gnu>/ld-linux-armhf.so.3": true,
	}
	var split []string
	for path, byLine := range lines {
		if len(byLine) > 1 && !really[path] {
			var alts []string
			for l, arches := range byLine {
				alts = append(alts, strings.Join(arches, ",")+": "+l)
			}
			sort.Strings(alts)
			split = append(split, path+"\n  "+strings.Join(alts, "\n  "))
		}
	}
	sort.Strings(split)
	assert.Len(t, split, 0, strings.Join(split, "\n"))
}
