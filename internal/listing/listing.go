// Package listing runs the cuts of a set of packages and lists every file
// each slice delivers with what file(1) says about it, per architecture,
// then folds architectures that agree.
package listing

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/lczyk/chisel-coverage/pkg/cuts"
	"github.com/lczyk/chisel-coverage/pkg/fold"
	"github.com/lczyk/chisel-coverage/pkg/packageslicesfile"

	"github.com/lczyk/chisel-file/internal/magic"
)

// DebSlice is the name the whole deb is listed under with Options.Deb.
const DebSlice = "deb"

// prefix names of the run's own slices in the synthesised file and
// the verbose lines: file-all, file-probe, file-ignore-<n>.
const prefix packageslicesfile.Names = "file"

// Options configures a run. Cuts is passed to cuts.Run with Whole,
// Optional and Visit set here.
type Options struct {
	Cuts    cuts.Options
	Backend magic.Backend
	Deb     bool // list the whole deb too, as DebSlice
}

// Arch is one package on one architecture: what cuts found, and per slice
// the listing of its files.
type Arch struct {
	Arch      string
	Available bool
	Err       error
	Version   string
	DebArch   string
	StandIn   string                   // the lead architecture this listing is copied from, for an arch: all package
	Slices    map[string][]magic.Entry // slice -> its files, sorted by path; DebSlice is the whole deb
}

// Package is one package across the architectures asked for.
type Package struct {
	Name   string
	Slices []string // the slices listed, in file order, DebSlice first when listed
	Arches []Arch   // in Options.Cuts.Arches order
}

type key struct{ pkg, arch, slice string }

// Run cuts every package on every architecture and lists each cut. A
// package with no package slices file is one with no slices: its deb
// is listed when the deb is, and --slice finds nothing of it. Run fails up
// front as cuts.Run does, and on a package whose real slice is named
// DebSlice when the whole deb is listed too.
func Run(ctx context.Context, opts Options, pkgs []string) ([]Package, error) {
	if opts.Backend == nil {
		return nil, errors.New("no backend")
	}
	if opts.Deb {
		for _, pkg := range pkgs {
			def, err := packageslicesfile.Read(opts.Cuts.Release, pkg)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if def.Slice(DebSlice) != nil {
				return nil, fmt.Errorf("%s has a slice named %q, which is what the whole deb is listed as", pkg, DebSlice)
			}
		}
	}
	var mu sync.Mutex
	listed := map[key][]magic.Entry{}
	var failed error
	co := opts.Cuts
	co.Whole = opts.Deb
	co.Optional = true
	co.Prefix = string(prefix)
	co.Visit = func(c cuts.Cut) {
		if prefix.IsIgnore(c.Slice) {
			return
		}
		entries, err := magic.List(opts.Backend, c.Root, c.Result.Paths)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if failed == nil {
				failed = fmt.Errorf("%s %s_%s: %w", c.Arch, c.Package, c.Slice, err)
			}
			return
		}
		listed[key{c.Package, c.Arch, c.Slice}] = entries
	}
	res, err := cuts.Run(ctx, co, pkgs)
	if err != nil {
		return nil, err
	}
	if failed != nil {
		return nil, failed
	}
	out := make([]Package, len(res))
	for i, p := range res {
		names := p.Slices
		if opts.Deb {
			names = append([]string{DebSlice}, names...)
		}
		out[i] = Package{Name: p.Name, Slices: names, Arches: make([]Arch, len(p.Arches))}
		for j, a := range p.Arches {
			out[i].Arches[j] = assemble(p.Name, names, opts.Deb, a, listed)
		}
	}
	return out, nil
}

// assemble is one architecture's listing: every slice's entries, from the
// lead architecture for a stand-in, with the ignored paths dropped. With
// deb, DebSlice is the whole-deb cut; otherwise it is a real slice.
func assemble(pkg string, names []string, deb bool, c cuts.Arch, listed map[key][]magic.Entry) Arch {
	a := Arch{Arch: c.Arch, Available: c.Available, Err: c.Err, Version: c.Version, DebArch: c.DebArch, StandIn: c.StandIn}
	if c.Err != nil || !c.Available {
		return a
	}
	from := c.Arch
	if c.StandIn != "" {
		from = c.StandIn
	}
	ignored := map[string]bool{}
	for name, paths := range c.Slices {
		if prefix.IsIgnore(name) {
			for _, p := range paths {
				ignored[p] = true
			}
		}
	}
	a.Slices = map[string][]magic.Entry{}
	for _, name := range names {
		cut := name
		if deb && name == DebSlice {
			cut = prefix.All()
		}
		entries := []magic.Entry{}
		for _, e := range listed[key{pkg, from, cut}] {
			if !ignored[e.Path] {
				entries = append(entries, e)
			}
		}
		a.Slices[name] = entries
	}
	return a
}

// Group is a block of the report: a set of architectures of one version
// and availability, and per slice the files they all have with the same
// line. Several architectures share normalised paths and lines; a group
// of one shows its raw ones.
type Group struct {
	Arches    []string
	Available bool
	Err       error
	Version   string
	DebArch   string
	Slices    []SliceGroup // in Package.Slices order; absent when not available or failed
}

// SliceGroup is one slice's files in a group.
type SliceGroup struct {
	Name  string
	Files []File
}

// File is one path with one line on every architecture of its group.
type File struct {
	Path string   // normalised when Raw has more than one, else the real path
	Line string   // normalised, so that what differs between blocks is what is left
	Raw  []string // each architecture's own line, in group order
}

// Groups folds a package's architectures of the same version and
// availability on what their slices hold: per slice, every (normalised
// path, normalised line) is keyed by the architectures it is on, and each
// key is a group, most architectures first. Groups of the same
// architectures across slices are one group with several slices. An
// arch: all package is one deb everywhere, so its paths are compared as
// they are. Architectures that are not available or whose cut failed are
// groups of their own with no slices.
func Groups(p Package) []Group {
	var members [][]Arch
	index := map[string]int{}
	for _, a := range p.Arches {
		k := fmt.Sprintf("%v\x00%s", a.Available, a.Version)
		if a.Err != nil {
			k = "err\x00" + a.Arch
		}
		if i, ok := index[k]; ok {
			members[i] = append(members[i], a)
			continue
		}
		index[k] = len(members)
		members = append(members, []Arch{a})
	}
	var groups []Group
	for _, m := range members {
		groups = append(groups, split(p.Slices, m)...)
	}
	return groups
}

// Ungrouped is one group per architecture, raw paths and raw lines.
func Ungrouped(p Package) []Group {
	groups := make([]Group, 0, len(p.Arches))
	for _, a := range p.Arches {
		g := Group{Arches: []string{a.Arch}, Available: a.Available, Err: a.Err, Version: a.Version, DebArch: a.DebArch}
		if a.Err == nil && a.Available {
			for _, name := range p.Slices {
				sg := SliceGroup{Name: name, Files: []File{}}
				for _, e := range a.Slices[name] {
					sg.Files = append(sg.Files, File{Path: e.Path, Line: e.Line, Raw: []string{e.Line}})
				}
				sort.Slice(sg.Files, func(i, j int) bool { return sg.Files[i].Path < sg.Files[j].Path })
				g.Slices = append(g.Slices, sg)
			}
		}
		groups = append(groups, g)
	}
	return groups
}

// split folds architectures of one version into groups by what their
// slices hold.
func split(names []string, arches []Arch) []Group {
	first := arches[0]
	if first.Err != nil || !first.Available {
		var out []Group
		for _, a := range arches {
			out = append(out, Group{Arches: []string{a.Arch}, Available: a.Available, Err: a.Err, Version: a.Version, DebArch: a.DebArch})
		}
		return out
	}
	order := make([]string, len(arches))
	for i, a := range arches {
		order[i] = a.Arch
	}
	index := map[string]int{}
	var groups []Group
	group := func(set []string) *Group {
		k := strings.Join(set, " ")
		gi, ok := index[k]
		if !ok {
			gi = len(groups)
			index[k] = gi
			groups = append(groups, Group{Arches: set, Available: true, Version: first.Version, DebArch: first.DebArch})
		}
		return &groups[gi]
	}
	for _, name := range names {
		items := make([][]fold.Item, len(arches))
		lines := map[string]string{} // arch + path -> file's own line
		empty := true
		var none []string // architectures where the slice has nothing
		for i, a := range arches {
			// An arch: all package is one deb everywhere, so nothing in it
			// names the architecture: paths and lines are compared as they are.
			arch := a.Arch
			if a.DebArch == "all" {
				arch = ""
			}
			for _, e := range a.Slices[name] {
				path := e.Path
				if arch != "" {
					path = fold.Normalise(e.Path, arch)
				}
				items[i] = append(items[i], fold.Item{Key: path, Tag: magic.Normalise(e.Line, arch), Raw: e.Path, RawTag: e.Line})
				lines[a.Arch+"\x00"+e.Path] = e.Line
				empty = false
			}
			if len(items[i]) == 0 {
				none = append(none, a.Arch)
			}
		}
		if empty {
			// A slice with nothing in it anywhere is still a slice: one
			// block, for every architecture, with no files.
			g := group(order)
			g.Slices = append(g.Slices, SliceGroup{Name: name, Files: []File{}})
			continue
		}
		for _, fg := range fold.Fold(order, items) {
			sg := SliceGroup{Name: name, Files: make([]File, 0, len(fg.Entries))}
			for _, e := range fg.Entries {
				f := File{Path: e.Shown(), Line: e.Tag, Raw: make([]string, len(e.Raw))}
				same := true
				for i, path := range e.Raw {
					f.Raw[i] = lines[fg.Arches[i]+"\x00"+path]
					same = same && f.Raw[i] == f.Raw[0]
				}
				// A symlink's target is a path: shown as it is when every
				// architecture has the same one, like a path is.
				if same && strings.HasPrefix(f.Raw[0], "symbolic link to ") {
					f.Line = f.Raw[0]
				}
				sg.Files = append(sg.Files, f)
			}
			g := group(fg.Arches)
			g.Slices = append(g.Slices, sg)
		}
		if len(none) > 0 {
			// Nothing on these architectures is a state of its own, one
			// block for them, so the slice is not silently theirs too.
			g := group(none)
			g.Slices = append(g.Slices, SliceGroup{Name: name, Files: []File{}})
		}
	}
	if len(names) == 0 {
		// Nothing to list is still a state these architectures share.
		group(order)
	}
	rank := map[string]int{}
	for i, a := range order {
		rank[a] = i
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i].Arches, groups[j].Arches
		if len(a) != len(b) {
			return len(a) > len(b)
		}
		for k := range a {
			if a[k] != b[k] {
				return rank[a[k]] < rank[b[k]]
			}
		}
		return false
	})
	return groups
}
