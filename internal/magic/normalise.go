package magic

import (
	"regexp"
	"strings"

	"github.com/lczyk/chisel-coverage/pkg/fold"

	"github.com/lczyk/chisel-file/internal/elfline"
)

var (
	buildID   = regexp.MustCompile(`, BuildID\[[^\]]*\]=[0-9a-fA-F]+`)
	forLinux  = regexp.MustCompile(`, for GNU/Linux [0-9.]+`)
	abiTag    = regexp.MustCompile(`,? version [0-9]+ \([^)]*\)`)
	gzipSize  = regexp.MustCompile(`, original size modulo 2\^32 [0-9]+`)
	gzipTime  = regexp.MustCompile(`, last modified: [^,]+`)
	longLines = regexp.MustCompile(`with very long lines \([0-9]+\)`)
	scriptOf  = regexp.MustCompile(` script text executable\b`)
	phrasing  = strings.NewReplacer("JSON data", "JSON text data")
	// A .NET ready-to-run image sets the PE machine to the target's
	// IMAGE_FILE_MACHINE xor 0x7b79 on linux, which libmagic prints as an
	// unknown processor: amd64 0xfd1d, arm64 0xd11d, armhf 0x7abd, i386
	// 0x7a35.
	readyToRun = regexp.MustCompile(`^PE32\+? executable for MS Windows [0-9.]+ \(([^)]*)\), Unknown processor 0x([0-9a-f]{4}) Mono/\.Net assembly(.*)$`)
	// A DER certificate is printed twice by two rules, the second with its
	// own idea of the version: "Certificate, Version=3 Certificate,
	// Version=01".
	certTwice = regexp.MustCompile(`^Certificate, Version=([0-9]+) Certificate, Version=[0-9]+`)
	// A shebang line file could not read as an interpreter: `#!"bash"`,
	// a template's "#!<%= ... %>", "#!." and the like become "a bash"
	// script", "a <%= ... %> script", "a . script".
	badShebang = regexp.MustCompile(`^a ([^ ]*[^A-Za-z0-9/._+= -][^ ]*|\.)( .*?)? (script|line)\b[^,]*`)
	// An EFI image names its machine the PE way and its section count,
	// which differs per build.
	efi = regexp.MustCompile(`^PE32\+? executable for EFI \(([^)]*)\), (x86-64|Intel 80386|ARM64|ARMv4T|ARMv7 Thumb|RISC-V 64-bit)( \(stripped to external PDB\))?, [0-9]+ sections$`)
	// The stat layer's words before anything else: "setuid ELF ...",
	// "setuid, setgid POSIX shell script ...", "setuid , ASCII text",
	// "setuid, empty".
	mode      = regexp.MustCompile(`^((?:setuid|setgid|sticky)(?:, (?:setuid|setgid|sticky))*)( ?,? )(.+)$`)
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

// guesses is what file says of plain text on keywords alone, with no magic
// to go by: a header is "C source" or "c program text" by how many keywords
// it saw, a go file any of five languages, a profile with braces a C
// program. They tell a reviewer nothing "text" does not, so they become it.
// A kind that names an interpreter (a script) or a real format (XML, JSON,
// gettext, troff's own) stays.
var guesses = []string{
	"c program", "C source", "C++ source", "Objective-C source",
	"Java source", "Nim source code", "Algol 68 source", "Lisp/Scheme program",
	"Pascal source", "assembler source",
	"Perl5 module source", "Perl POD document", "awk or perl script", "Ruby script",
	"HTML document", "exported SGML document", "CSV", "unified diff output",
	"makefile script", "BSD makefile script", "M4 macro processor script",
	"troff or preprocessor input", "DOS batch file",
	"JavaScript source", "Typst source", "OS/2 REXX batch file", "SAS 7+",
}

var (
	r2rMachines = map[string]string{"fd1d": "amd64", "d11d": "arm64", "7abd": "armhf", "7a35": "i386"}
	peMachines  = map[string]string{"x86-64": "amd64", "Intel 80386": "i386", "ARM64": "arm64", "ARMv4T": "armhf", "ARMv7 Thumb": "armhf", "RISC-V 64-bit": "riscv64"}
)

// archWord is <arch> for the cut's own architecture and <other:name> for
// another.
func archWord(name, arch string) string {
	if name == arch {
		return "<arch>"
	}
	return "<other:" + name + ">"
}

// machineWord is <arch> when the ready-to-run code is the cut's own
// architecture's, <other:name> for another known one, and the code itself
// for one not in the table.
func machineWord(code, arch string) string {
	name := r2rMachines[code]
	if name == "" {
		return "unknown processor 0x" + code
	}
	return archWord(name, arch)
}

// guessed replaces a guessed kind at the start of line with "text": the
// phrase, then " text" if it is there, then the end or a comma, and no
// "executable" after it: "Ruby script text" is a guess, "Ruby script text
// executable" and "Ruby script, ASCII text executable" have a shebang.
func guessed(line string) string {
	for _, g := range guesses {
		rest, ok := strings.CutPrefix(line, g)
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(rest, " text")
		if (rest == "" || strings.HasPrefix(rest, ",")) && !strings.Contains(rest, "executable") {
			return "text" + rest
		}
	}
	// "#![allow(unused)]" in a rust source is read as a shebang:
	// "a [allow(unused)] script text executable".
	if strings.HasPrefix(line, "a [") {
		if _, rest, ok := strings.Cut(line, " script text executable"); ok {
			return "text" + rest
		}
	}
	return line
}

// Normalise rewrites a file(1) line so that the same kind of file gets the
// same line on every architecture. An ELF description is rendered short by
// elfline for the cut's architecture (arch), a symlink's target is
// normalised as a path. In any other line build ids,
// kernel versions, abi tags, text encodings, gzip's size and date and the
// length of the longest line go, a language guessed from keywords becomes
// "text", and phrasing that moved between libmagic releases ("Python
// script text executable" / "Python script, ASCII text executable", "JSON
// data" / "JSON text data", a kind printed twice) is written one way.
func Normalise(line, arch string) string {
	// A symlink's target is a path, which names the architecture the way
	// paths do.
	if target, ok := strings.CutPrefix(line, "symbolic link to "); ok {
		return "symbolic link to " + fold.Normalise(target, arch)
	}
	if l, ok := elfline.Parse(line); ok {
		return l.Short(arch)
	}
	// The mode words stay as they are, in front of the rest normalised.
	if m := mode.FindStringSubmatch(line); m != nil {
		return m[1] + m[2] + Normalise(m[3], arch)
	}
	if m := efi.FindStringSubmatch(line); m != nil {
		return "PE " + archWord(peMachines[m[2]], arch) + " EFI " + m[1] + m[3]
	}
	if m := readyToRun.FindStringSubmatch(line); m != nil {
		return "PE " + machineWord(m[2], arch) + " Mono/.Net assembly (" + m[1] + ")" + m[3]
	}
	line = buildID.ReplaceAllString(line, "")
	line = forLinux.ReplaceAllString(line, "")
	line = abiTag.ReplaceAllString(line, "")
	line = gzipSize.ReplaceAllString(line, "")
	line = gzipTime.ReplaceAllString(line, "")
	line = longLines.ReplaceAllString(line, "with very long lines")
	line = once(line)
	line = guessed(line)
	line = certTwice.ReplaceAllString(line, "Certificate, Version=$1")
	line = badShebang.ReplaceAllString(line, "text")
	line = scriptOf.ReplaceAllString(line, " script, text executable")
	line = phrasing.Replace(line)
	line = encodings.Replace(line)
	return dedupe(line, ", text")
}

// once drops a kind printed twice in a row, as file does when two rules
// match the same thing: "Python script text executable Python script text
// executable" (5.41, a shebang and keywords), "XHTML document text (version
// 1.0) XHTML document text (version 1.0)" and "HTML document text HTML
// document" (5.48). The kind is the longest prefix that is followed by
// itself, with or without its trailing "text", and then a comma or the end.
func once(line string) string {
	for i := len(line) / 2; i > 0; i-- {
		if line[i] != ' ' {
			continue
		}
		kind := line[:i]
		for _, again := range []string{kind, strings.TrimSuffix(kind, " text")} {
			rest, ok := strings.CutPrefix(line[i+1:], again)
			if ok && (rest == "" || strings.HasPrefix(rest, ",")) {
				return kind + rest
			}
		}
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
	// The same in the middle: "XHTML document text, text, with very long
	// lines".
	if i := strings.Index(line, ", text, "); i >= 0 && strings.Contains(line[:i], "text") {
		line = line[:i] + line[i+len(", text"):]
	}
	return strings.ReplaceAll(line, "text, text", "text")
}
