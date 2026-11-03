// Package elfline reads what file(1) says about an ELF binary and renders
// it short: the architecture's words replaced by what names it, the
// filler gone, the kind, the linkage and the stripping kept.
package elfline

import (
	"regexp"
	"strings"
)

// Line is file(1)'s ELF description, field by field.
type Line struct {
	Mode     string   // the stat layer's words before ELF: "setuid", "setuid, setgid"
	Class    string   // "32-bit" or "64-bit"
	Endian   string   // "LSB" or "MSB"
	Type     string   // relocatable, executable, pie executable, shared object, core file
	Machine  string   // as file prints it: "x86-64", "ARM, EABI5", "UCB RISC-V, RVC, double-float ABI"
	ABI      string   // from "version 1 (<abi>)": SYSV, GNU/Linux
	Linkage  string   // statically linked, dynamically linked, static-pie linked
	Interp   string   // the loader, when dynamically linked with one
	BuildID  string   // "BuildID[sha1]=..." as printed
	Kernel   string   // from "for GNU/Linux <kernel>"
	Extras   []string // anything else, in order: "with debug_info", "no section header", ...
	Stripped string   // "stripped" or "not stripped", "" when file said neither
}

// arch is what names an architecture in an ELF description: the machine as
// file prints it and the loader its binaries name. From file 5.48 on ubuntu
// 24.04.
type known struct {
	name    string // the deb architecture
	machine string
	loader  string
}

// arches is scanned in order, so a machine that is a prefix of another's
// must come after it.
var arches = []known{
	{"amd64", "x86-64", "/lib64/ld-linux-x86-64.so.2"},
	{"arm64", "ARM aarch64", "/lib/ld-linux-aarch64.so.1"},
	{"armhf", "ARM, EABI5", "/lib/ld-linux-armhf.so.3"},
	{"i386", "Intel i386", "/lib/ld-linux.so.2"},
	{"ppc64el", "64-bit PowerPC or cisco 7500, OpenPOWER ELF V2 ABI", "/lib64/ld64.so.2"},
	{"riscv64", "UCB RISC-V, RVC, double-float ABI", "/lib/ld-linux-riscv64-lp64d.so.1"},
	{"s390x", "IBM S/390", "/lib/ld64.so.1"},
}

// The head of the line up to the abi tag. The machine may hold commas and
// armhf's "ARM, EABI5" has no comma before "version", so the machine is
// whatever sits between the type and the tag.
var head = regexp.MustCompile(`^(?:(.*?) )?ELF (32|64)-bit (LSB|MSB) (relocatable|executable|pie executable|shared object|core file), (.+?),? version [0-9]+ \(([^)]*)\)(.*)$`)

// Parse reads a file(1) ELF description. ok is false for any other line.
func Parse(line string) (l Line, ok bool) {
	m := head.FindStringSubmatch(line)
	if m == nil {
		return Line{}, false
	}
	l = Line{Mode: m[1], Class: m[2] + "-bit", Endian: m[3], Type: m[4], Machine: m[5], ABI: m[6]}
	rest := strings.TrimPrefix(m[7], ", ")
	if rest == "" {
		return l, true
	}
	for _, field := range strings.Split(rest, ", ") {
		switch {
		case field == "statically linked", field == "dynamically linked", field == "static-pie linked":
			l.Linkage = field
		case strings.HasPrefix(field, "interpreter "):
			l.Interp = strings.TrimPrefix(field, "interpreter ")
		case strings.HasPrefix(field, "BuildID["):
			l.BuildID = field
		case strings.HasPrefix(field, "for GNU/Linux "):
			l.Kernel = strings.TrimPrefix(field, "for GNU/Linux ")
		case field == "stripped", field == "not stripped":
			l.Stripped = field
		default:
			l.Extras = append(l.Extras, field)
		}
	}
	return l, true
}

// Arch is the deb architecture the machine names, "" for one not in the
// table.
func (l Line) Arch() string {
	for _, a := range arches {
		if l.Machine == a.machine {
			return a.name
		}
	}
	return ""
}

var (
	types = map[string]string{
		"relocatable":    "obj",
		"executable":     "exec",
		"pie executable": "pie",
		"shared object":  "lib",
		"core file":      "core",
	}
	linkages = map[string]string{
		"statically linked":  "static",
		"dynamically linked": "dynamic",
		"static-pie linked":  "static-pie",
	}
)

// Short renders the line for a cut on arch, dropping what carries no
// information and replacing what the architecture determines: the class,
// endianness and machine become <arch> when they are arch's own and
// <other:<name>> when another known architecture's, and stay as they are
// for a machine not in the table; the loader becomes <ld> when it is the
// architecture's own, else stays as its path. The type and linkage are one
// word each, the abi, build id and kernel version go, the extras and the
// stripping stay.
//
//	ELF <arch> pie, dynamic, <ld>, stripped
//	setuid ELF <arch> exec, static, stripped
//	ELF <other:amd64> lib, dynamic, stripped
//	ELF 32-bit MSB exec, MIPS, MIPS32, static, stripped
func (l Line) Short(arch string) string {
	var b strings.Builder
	if l.Mode != "" {
		b.WriteString(l.Mode + " ")
	}
	b.WriteString("ELF ")
	var own *known
	for i := range arches {
		if arches[i].machine == l.Machine {
			own = &arches[i]
			break
		}
	}
	kind := types[l.Type]
	if kind == "" {
		kind = l.Type
	}
	switch {
	case own == nil:
		b.WriteString(l.Class + " " + l.Endian + " " + kind + ", " + l.Machine)
	case own.name == arch:
		b.WriteString("<arch> " + kind)
	default:
		b.WriteString("<other:" + own.name + "> " + kind)
	}
	if l.Linkage != "" {
		if short, ok := linkages[l.Linkage]; ok {
			b.WriteString(", " + short)
		} else {
			b.WriteString(", " + l.Linkage)
		}
	}
	if l.Interp != "" {
		if own != nil && own.name == arch && l.Interp == own.loader {
			b.WriteString(", <ld>")
		} else {
			b.WriteString(", " + l.Interp)
		}
	}
	for _, e := range l.Extras {
		b.WriteString(", " + e)
	}
	if l.Stripped != "" {
		b.WriteString(", " + l.Stripped)
	}
	return b.String()
}
