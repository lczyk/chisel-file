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
	line = buildID.ReplaceAllString(line, "")
	line = forLinux.ReplaceAllString(line, "")
	line = abiTag.ReplaceAllString(line, "")
	line = gzipSize.ReplaceAllString(line, "")
	line = gzipTime.ReplaceAllString(line, "")
	line = longLines.ReplaceAllString(line, "with very long lines")
	line = guessed(line)
	line = once(line, " script text executable")
	line = scriptOf.ReplaceAllString(line, " script, text executable")
	line = phrasing.Replace(line)
	line = encodings.Replace(line)
	return dedupe(line, ", text")
}

// once drops a kind printed twice in a row, as file 5.41 does for a python
// file whose shebang and keywords both match: "Python script text
// executable Python script text executable".
func once(line, suffix string) string {
	i := strings.Index(line, suffix)
	if i < 0 {
		return line
	}
	kind := line[:i+len(suffix)]
	if rest, ok := strings.CutPrefix(line[len(kind):], " "+kind); ok {
		return kind + rest
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
