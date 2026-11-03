package magic

import (
	"regexp"
	"strings"
)

// elfArch is what names an architecture in an ELF description: the class
// and endianness, the machine as file prints it, and the loader a
// dynamically linked binary names.
type elfArch struct {
	class   string // "64-bit LSB"
	machine string // "x86-64"
	loader  string // "/lib64/ld-linux-x86-64.so.2"
}

// elfArches is per deb architecture, from file 5.48 on ubuntu 24.04. A
// machine that is a prefix of another's must come after it, since the
// table is scanned in order.
var elfArches = []struct {
	arch string
	elfArch
}{
	{"amd64", elfArch{"64-bit LSB", "x86-64", "/lib64/ld-linux-x86-64.so.2"}},
	{"arm64", elfArch{"64-bit LSB", "ARM aarch64", "/lib/ld-linux-aarch64.so.1"}},
	{"armhf", elfArch{"32-bit LSB", "ARM, EABI5", "/lib/ld-linux-armhf.so.3"}},
	{"i386", elfArch{"32-bit LSB", "Intel i386", "/lib/ld-linux.so.2"}},
	{"ppc64el", elfArch{"64-bit LSB", "64-bit PowerPC or cisco 7500, OpenPOWER ELF V2 ABI", "/lib64/ld64.so.2"}},
	{"riscv64", elfArch{"64-bit LSB", "UCB RISC-V, RVC, double-float ABI", "/lib/ld-linux-riscv64-lp64d.so.1"}},
	{"s390x", elfArch{"64-bit MSB", "IBM S/390", "/lib/ld64.so.1"}},
}

var (
	buildID   = regexp.MustCompile(`, BuildID\[[^\]]*\]=[0-9a-fA-F]+`)
	forLinux  = regexp.MustCompile(`, for GNU/Linux [0-9.]+`)
	abiTag    = regexp.MustCompile(`,? version [0-9]+ \([^)]*\)`)
	gzipSize  = regexp.MustCompile(`, original size modulo 2\^32 [0-9]+`)
	gzipTime  = regexp.MustCompile(`, last modified: [^,]+`)
	elfHead   = regexp.MustCompile(`^(.*\bELF )(32|64)-bit (LSB|MSB) ([a-z ]+), `)
	encodings = strings.NewReplacer(
		"Unicode text, UTF-8 text", "text",
		"Unicode text, UTF-8 (with BOM) text", "text",
		"UTF-8 Unicode text", "text",
		"UTF-8 text", "text",
		"ASCII text", "text",
		"ISO-8859 text", "text",
		"Non-ISO extended-ASCII text", "text",
	)
)

// Normalise rewrites a file(1) line so that the same kind of file gets the
// same line on every architecture: build ids, kernel versions, abi tags,
// text encodings and gzip's size and date go; the ELF class, endianness
// and machine become <arch> for the cut's architecture (arch) and
// <other:<name>> for another ubuntu architecture's, and the architecture's
// own loader becomes <ld>. Everything else stays as file printed it.
func Normalise(line, arch string) string {
	line = buildID.ReplaceAllString(line, "")
	line = forLinux.ReplaceAllString(line, "")
	line = abiTag.ReplaceAllString(line, "")
	line = gzipSize.ReplaceAllString(line, "")
	line = gzipTime.ReplaceAllString(line, "")
	line = encodings.Replace(line)
	line = dedupe(line, ", text")
	if strings.Contains(line, "ELF ") {
		line = normaliseELF(line, arch)
	}
	return line
}

// normaliseELF replaces the machine words with <arch> or <other:...>.
func normaliseELF(line, arch string) string {
	m := elfHead.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	rest := line[len(m[0]):]
	for _, a := range elfArches {
		if !strings.HasPrefix(rest, a.machine) {
			continue
		}
		rest = strings.TrimPrefix(rest, a.machine)
		rest = strings.TrimPrefix(rest, ", ")
		if a.arch != arch {
			// Another architecture's binary: loud, and its loader as it is.
			return m[1] + "<other:" + a.arch + "> " + m[4] + ", " + rest
		}
		rest = strings.Replace(rest, "interpreter "+a.loader, "interpreter <ld>", 1)
		return m[1] + "<arch> " + m[4] + ", " + rest
	}
	return line
}

// dedupe collapses a ", text" that follows a description already saying
// what the text is ("Python script, text executable, text"), and a
// repeated one.
func dedupe(line, suffix string) string {
	for strings.HasSuffix(line, suffix) {
		head := strings.TrimSuffix(line, suffix)
		if head == "" || head == "text" || strings.HasSuffix(head, ", ") {
			break
		}
		if !strings.Contains(head, "text") {
			break
		}
		line = head
	}
	return strings.ReplaceAll(line, "text, text", "text")
}
