package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lczyk/chisel-file/internal/listing"
)

// style colours the text report. The zero value colours nothing.
type style struct {
	name, dim, red, yellow, reset string
}

// colours is the style for a terminal: a little, not a lot.
var colours = style{name: "\x1b[1;34m", dim: "\x1b[2m", red: "\x1b[31m", yellow: "\x1b[33m", reset: "\x1b[0m"}

// styleFor picks colours when w is a terminal and NO_COLOR is unset or empty
// (https://no-color.org), else nothing.
func styleFor(w io.Writer) style {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return style{}
	}
	f, ok := w.(*os.File)
	if !ok {
		return style{}
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return style{}
	}
	return colours
}

func (s style) wrap(code, text string) string {
	if code == "" {
		return text
	}
	return code + text + s.reset
}

// renderText prints one block per (package, slice, group of architectures),
// or per architecture when group is false: a header line, then one
// "<path>: <line>" per file, a symlink as "<path> -> <target>". Nothing is indented and no line is blank, so
// the output greps and pipes. A package that is not available or whose cut
// failed is one line per such architecture, before its slices.
func renderText(w io.Writer, pkgs []listing.Package, group bool, s style) {
	for _, p := range pkgs {
		groups := listing.Ungrouped(p)
		if group {
			groups = listing.Groups(p)
		}
		for _, g := range groups {
			renderGroup(w, p, g, s)
		}
	}
}

func renderGroup(w io.Writer, p listing.Package, g listing.Group, s style) {
	name := s.wrap(s.name, p.Name)
	arches := s.wrap(s.dim, "["+strings.Join(g.Arches, ", ")+"]")
	switch {
	case g.Err != nil:
		fmt.Fprintf(w, "%s %s: %s\n  %s\n", name, arches, s.wrap(s.red, "error"), strings.ReplaceAll(g.Err.Error(), "\n", "\n  "))
		return
	case !g.Available:
		fmt.Fprintf(w, "%s %s: %s\n", name, arches, s.wrap(s.yellow, "not available"))
		return
	}
	version := g.Version
	if g.DebArch == "all" {
		version += " (all)"
	}
	for _, sg := range g.Slices {
		head := name
		if version != "" {
			head += " " + version
		}
		fmt.Fprintf(w, "%s %s %s\n", head, sg.Name, arches)
		for _, f := range sg.Files {
			if target, ok := strings.CutPrefix(f.Line, "symbolic link to "); ok {
				fmt.Fprintf(w, "%s -> %s\n", f.Path, target)
				continue
			}
			fmt.Fprintf(w, "%s: %s\n", f.Path, f.Line)
		}
	}
}

// runInfo names what produced the json: the backend and its libmagic, the
// chisel, the release as given.
type runInfo struct {
	Backend string `json:"backend"`
	Magic   string `json:"magic"`
	Chisel  string `json:"chisel"`
	Release string `json:"release"`
}

type jsonOut struct {
	Run      runInfo       `json:"run"`
	Packages []jsonPackage `json:"packages"`
}

type jsonPackage struct {
	Name   string      `json:"name"`
	Arches []jsonArch  `json:"arches"`
	Slices []jsonSlice `json:"slices"`
}

type jsonArch struct {
	Arch      string  `json:"arch"`
	Available bool    `json:"available"`
	Version   *string `json:"version"`
	DebArch   *string `json:"deb-arch"`
	Error     *string `json:"error"`
}

type jsonSlice struct {
	Name  string     `json:"name"`
	Files []jsonFile `json:"files"`
}

type jsonFile struct {
	Path   string            `json:"path"`
	Arches []string          `json:"arches"`
	File   string            `json:"file"`
	Raw    map[string]string `json:"raw,omitempty"`
}

// renderJSON prints the folded listing: per package the architectures'
// state, then per slice one record per (path, line, architectures). The
// line is normalised, the path too when the record spans several
// architectures; with raw, every record also carries each architecture's
// own line.
func renderJSON(w io.Writer, run runInfo, pkgs []listing.Package, raw bool) error {
	out := jsonOut{Run: run, Packages: []jsonPackage{}}
	for _, p := range pkgs {
		jp := jsonPackage{Name: p.Name, Arches: []jsonArch{}, Slices: []jsonSlice{}}
		for _, a := range p.Arches {
			ja := jsonArch{Arch: a.Arch, Available: a.Available, Version: optional(a.Version), DebArch: optional(a.DebArch)}
			if a.Err != nil {
				ja.Error = optional(a.Err.Error())
			}
			jp.Arches = append(jp.Arches, ja)
		}
		slices := map[string]*jsonSlice{}
		for _, name := range p.Slices {
			js := &jsonSlice{Name: name, Files: []jsonFile{}}
			slices[name] = js
		}
		for _, g := range listing.Groups(p) {
			for _, sg := range g.Slices {
				for _, f := range sg.Files {
					jf := jsonFile{Path: f.Path, Arches: g.Arches, File: f.Line}
					if raw {
						jf.Raw = map[string]string{}
						for i, arch := range g.Arches {
							jf.Raw[arch] = f.Raw[i]
						}
					}
					slices[sg.Name].Files = append(slices[sg.Name].Files, jf)
				}
			}
		}
		for _, name := range p.Slices {
			jp.Slices = append(jp.Slices, *slices[name])
		}
		out.Packages = append(out.Packages, jp)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
