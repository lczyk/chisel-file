package magic

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"unicode"
	"unicode/utf8"

	"code.eitanity.com/softmagic"
)

// builtinBackend is softmagic's port of libmagic with its embedded database,
// plus the stat layer file(1) puts in front of what the rules say.
type builtinBackend struct {
	db      *softmagic.Database
	version string
}

// NewBuiltin loads the embedded database once.
func NewBuiltin() (Backend, error) {
	db, err := softmagic.Default()
	if err != nil {
		return nil, fmt.Errorf("builtin backend: %w", err)
	}
	version := "softmagic"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "code.eitanity.com/softmagic" {
				version += " " + d.Version
			}
		}
	}
	return &builtinBackend{db: db, version: version + " (file " + softmagic.ImplementsFile + ")"}, nil
}

func (b *builtinBackend) Name() string    { return "builtin" }
func (b *builtinBackend) Version() string { return b.version }

func (b *builtinBackend) Identify(root string, paths []string) ([]string, error) {
	lines := make([]string, len(paths))
	for i, p := range paths {
		lines[i] = b.identify(filepath.Join(root, p))
	}
	return lines, nil
}

// identify is what file(1) prints for one regular file: the mode words,
// "empty" for nothing to read, else the rules' answer with the executable
// bit in hand, joined after the mode words the way the reference joins them.
func (b *builtinBackend) identify(name string) string {
	info, err := os.Lstat(name)
	if err != nil {
		return "error: " + err.Error()
	}
	prefix := modeWords(info.Mode())
	if info.Size() == 0 {
		if prefix != "" {
			return prefix + ", empty"
		}
		return "empty"
	}
	if prefix != "" {
		prefix += " "
	}
	f, err := os.Open(name)
	if err != nil {
		return "error: " + err.Error()
	}
	defer f.Close()
	r := b.db.IdentifyAt(context.Background(), f, info.Size(), softmagic.Options{Executable: info.Mode()&0o111 != 0})
	if r.Examined.ReadError != nil {
		return "error: " + r.Examined.ReadError.Error()
	}
	if r.Failures.Description.Failed() {
		return "error: " + r.Failures.Description.Text(prefix)
	}
	// The text phase sees the mode words as output already made and puts
	// ", " before its own description unless a text rule printed something.
	if prefix != "" && r.Phase == softmagic.PhaseText && len(r.Rules) == 0 {
		return escape(prefix + ", " + r.Description)
	}
	return escape(prefix + r.Description)
}

// modeWords is file(1)'s "setuid", "setgid", "sticky" list.
func modeWords(m fs.FileMode) string {
	var words []string
	if m&fs.ModeSetuid != 0 {
		words = append(words, "setuid")
	}
	if m&fs.ModeSetgid != 0 {
		words = append(words, "setgid")
	}
	if m&fs.ModeSticky != 0 {
		words = append(words, "sticky")
	}
	return strings.Join(words, ", ")
}

// escape is file(1)'s output escaping: in valid UTF-8 every unprintable
// character becomes the octal escapes of its bytes, and text that is not
// UTF-8 is escaped byte by byte outside printable ASCII.
func escape(text string) string {
	if !utf8.ValidString(text) {
		var b strings.Builder
		for i := 0; i < len(text); i++ {
			if c := text[i]; c >= 0x20 && c < 0x7f {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, "\\%03o", c)
			}
		}
		return b.String()
	}
	clean := true
	for _, r := range text {
		if !printable(r) {
			clean = false
			break
		}
	}
	if clean {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if printable(r) {
			b.WriteString(text[i : i+size])
		} else {
			for k := 0; k < size; k++ {
				fmt.Fprintf(&b, "\\%03o", text[i+k])
			}
		}
		i += size
	}
	return b.String()
}

func printable(r rune) bool {
	return r == ' ' || unicode.IsPrint(r)
}
