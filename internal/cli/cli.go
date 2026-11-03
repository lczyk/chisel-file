// Package cli parses arguments, runs the listing and renders the report.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	goflags "github.com/jessevdk/go-flags"
	ver "github.com/lczyk/version/go"

	"github.com/lczyk/chisel-coverage/pkg/cache"
	"github.com/lczyk/chisel-coverage/pkg/cut"
	"github.com/lczyk/chisel-coverage/pkg/cuts"
	"github.com/lczyk/chisel-coverage/pkg/release"

	chiselfile "github.com/lczyk/chisel-file"
	"github.com/lczyk/chisel-file/internal/listing"
	"github.com/lczyk/chisel-file/internal/magic"
)

// Main runs the CLI and returns a process exit code: 0 ran fine, 2 a usage
// error, no backend or a failed cut, 130 interrupted.
func Main(args []string) int {
	if len(args) == 0 {
		_, _, _ = parseArgs([]string{"--help"}, os.Stderr)
		return 2
	}
	switch args[0] {
	case "version":
		args = []string{"--version"}
	case "help":
		args = []string{"--help"}
	}

	f, pkgs, err := parseArgs(args, os.Stdout)
	if err != nil {
		errf(err)
		return 2
	}
	if f.help {
		return 0
	}
	if f.Version {
		fmt.Println(ver.Read(fileVersion(chiselfile.VersionFile)))
		return 0
	}
	if len(pkgs) == 0 {
		errf(fmt.Errorf("no packages given"))
		return 2
	}
	arches, err := cuts.ResolveArches(f.Arches)
	if err != nil {
		errf(err)
		return 2
	}
	if f.Jobs == 0 || f.Jobs < -1 {
		errf(fmt.Errorf("--jobs must be at least 1, or -1 for one per cpu"))
		return 2
	}
	backend, err := magic.Pick(f.Backend)
	if err != nil {
		errf(err)
		return 2
	}
	// --slice all is the whole deb alone; a real slice named all is refused
	// by the run when --all is given, and is just a slice otherwise.
	only, all := f.Slices, f.All
	if i := indexOf(only, listing.AllSlice); i >= 0 {
		all = true
		only = append(only[:i:i], only[i+1:]...)
		if len(only) == 0 {
			only = []string{}
		}
	}

	// On the first interrupt the cuts stop and the temp dirs go; a second
	// one ends the process at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()
	runner := &cut.Runner{Bin: cut.Binary()}
	if len(f.Verbose) >= 2 {
		runner.Verbose = os.Stderr
	}
	dir, err := release.Resolve(ctx, f.Release, cache.Dir(), runner.Fetch, func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "%s %s\n", cut.Stamp(), fmt.Sprintf(format, args...))
	})
	if err != nil {
		errf(err)
		return 2
	}

	opts := listing.Options{
		Backend: backend,
		All:     all,
		Cuts: cuts.Options{
			Release: dir,
			Arches:  arches,
			Only:    only,
			Ignores: f.Ignores,
			Keep:    f.Keep,
			Jobs:    f.Jobs,
			Cache:   cache.Dir(),
			Runner:  runner,
			Log:     os.Stderr,
		},
	}
	if len(f.Verbose) >= 1 {
		opts.Cuts.Verbose = os.Stderr
	}
	results, err := listing.Run(ctx, opts, pkgs)
	if ctx.Err() != nil {
		errf(errors.New("interrupted"))
		return 130
	}
	if err != nil {
		errf(err)
		return 2
	}

	if f.JSON {
		run := runInfo{Backend: backend.Name(), Magic: backend.Version(), Chisel: chiselVersion(runner.Bin), Release: f.Release}
		if err := renderJSON(os.Stdout, run, results, f.Raw); err != nil {
			errf(err)
			return 2
		}
	} else {
		renderText(os.Stdout, results, !f.NoGroup && !f.Raw, styleFor(os.Stdout))
	}

	for _, p := range results {
		for _, a := range p.Arches {
			if a.Err != nil {
				return 2
			}
		}
	}
	return 0
}

// flags is the command line; go-flags fills it and renders the help from it.
type flags struct {
	Release string   `long:"release" value-name:"<dir|name>" default:"." description:"chisel-releases checkout, or a release name like ubuntu-24.04 fetched by chisel into its cache"`
	Arches  []string `long:"arch" value-name:"<a>[,<a>...]" description:"architectures to list; \"all\" for every one (default: host)"`
	Slices  []string `long:"slice" value-name:"<s>[,<s>...]" description:"only these slices, bare or <package>_<slice>; \"all\" is the whole deb (default: all slices)"`
	Ignores []string `long:"ignore" value-name:"<glob>[,...]" description:"leave matching paths out (chisel glob syntax)"`
	All     bool     `long:"all" description:"list the whole deb too, as slice \"all\"; works without a slice definition file"`
	Backend string   `long:"backend" value-name:"<file|builtin>" description:"what runs file: the binary on PATH, or the built-in port (default: file when found)"`
	Raw     bool     `long:"raw" description:"show file's lines as they are, no normalising; with --json, add every architecture's raw line"`
	NoGroup bool     `long:"no-group" description:"one block per architecture, raw paths and lines, instead of folding architectures with the same result"`
	JSON    bool     `long:"json" description:"machine-readable output, folded"`
	Keep    bool     `long:"keep" description:"keep the temp releases and rootfs dirs"`
	Jobs    int      `short:"j" long:"jobs" value-name:"<n>" default:"-1" description:"cuts to run at once; -1 for one per cpu"`
	Verbose []bool   `short:"v" long:"verbose" description:"one line per cut on stderr; twice also streams chisel's output"`
	Version bool     `long:"version" description:"print the version and exit"`

	help bool // -h or --help was given
}

func newParser(f *flags) *goflags.Parser {
	p := goflags.NewNamedParser("chisel-file", goflags.HelpFlag|goflags.PassDoubleDash)
	p.Usage = "[flags] <package>..."
	p.LongDescription = strings.TrimSpace(`
for each package, every slice is cut on its own against a throwaway copy of
the release with only that package in it, and file is run over every path
the cut delivers. the "chisel" binary is found on PATH, or set
CHISEL=/path/to/chisel; the "file" binary likewise, or FILE=/path/to/file.
"version" and "help" as the first argument are --version and --help.`)
	if _, err := p.AddGroup("flags", "", f); err != nil {
		panic(err)
	}
	return p
}

// parseArgs parses the command line. List flags take comma-separated values
// and may repeat; -h, --help and --version work wherever they land on the
// line. Help is not an error: the usage goes to help, f.help is set and the
// caller stops.
func parseArgs(args []string, help io.Writer) (f flags, pkgs []string, err error) {
	p := newParser(&f)
	pkgs, err = p.ParseArgs(args)
	if err != nil {
		var fe *goflags.Error
		if errors.As(err, &fe) && fe.Type == goflags.ErrHelp {
			p.WriteHelp(help)
			f.help = true
			return f, nil, nil
		}
		return f, nil, err
	}
	for _, l := range []*[]string{&f.Arches, &f.Slices, &f.Ignores} {
		*l = splitLists(*l)
	}
	switch f.Backend {
	case "", "file", "builtin":
	default:
		return f, nil, fmt.Errorf("--backend must be file or builtin, not %q", f.Backend)
	}
	return f, pkgs, nil
}

// splitLists splits every value of a repeatable list flag on commas and drops
// the blanks, so --arch amd64,arm64 and --arch amd64 --arch arm64 are the same.
func splitLists(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// chiselVersion is what "chisel version" prints, "" when it cannot be run.
func chiselVersion(bin string) string {
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fileVersion returns the version line of a VERSION file: the first line that
// is neither blank nor a # comment.
func fileVersion(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func errf(err error) {
	fmt.Fprintf(os.Stderr, "chisel-file: %v\n", err)
}
