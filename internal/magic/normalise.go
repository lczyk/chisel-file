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
// same line on every architecture. An ELF description is rendered short by
// elfline for the cut's architecture (arch), a symlink's target is
// normalised as a path. In any other line build ids,
// kernel versions, abi tags, text encodings, gzip's size and date and the
// length of the longest line go, and the rest stays as file printed it.
func Normalise(line, arch string) string {
	if l, ok := elfline.Parse(line); ok {
		return l.Short(arch)
	}
	// A symlink's target is a path, which names the architecture the way
	// paths do.
	if target, ok := strings.CutPrefix(line, "symbolic link to "); ok {
		return "symbolic link to " + fold.Normalise(target, arch)
	}
	line = buildID.ReplaceAllString(line, "")
	line = forLinux.ReplaceAllString(line, "")
	line = abiTag.ReplaceAllString(line, "")
	line = gzipSize.ReplaceAllString(line, "")
	line = gzipTime.ReplaceAllString(line, "")
	line = longLines.ReplaceAllString(line, "with very long lines")
	line = encodings.Replace(line)
	return dedupe(line, ", text")
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
