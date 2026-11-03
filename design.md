# chisel-file: design

a development tool for [chisel-releases](https://github.com/canonical/chisel-releases) work. for a package, it runs `file` over every path each slice delivers, per architecture, and reports what kind of thing every path is. information only: no rules, no verdicts, no exit code for findings.

this file is a working note for the first implementation. it goes away once the readme carries what a user needs; the reasoning stays in git history.

## principle

chisel does the cutting and `file` does the judging. the tool never reads an archive, extracts a `.deb` or matches a glob, and it never decides what a file is: every listing is a `chisel cut` of a throwaway release walked by [`chisel-coverage/pkg`](https://github.com/lczyk/chisel-coverage), and every description is `file`'s, either from the `file` binary on the host or from [softmagic](https://github.com/eitanity/softmagic), a pure-go port of libmagic that gives byte-identical answers for the same database. the tool's own work is listing, normalising and folding.

## how it works

for each package:

1. `chisel-coverage/pkg/cuts` cuts every slice of the package's sdf on every architecture asked for, stripped the way chisel-coverage strips them: essentials, mutate scripts, `text` / `symlink` / `make` / `generate` entries gone, `copy` entries re-keyed by their source. so a listing is what the deb puts in the slice, nothing from other packages and nothing generated. `--ignore` globs become extra slices whose paths are dropped from every listing. `--all` adds the whole-deb slice, listed as `all`, and works for a package that has no sdf yet.
2. while each cut's rootfs exists, every regular file in it goes through the backend and every symlink is read: the line for a symlink is `symbolic link to <target>` from the tool itself, not from `file`, because `file` resolves the target against the host's `/`, which is not the rootfs. directories are not listed: the implicit parents of a file are what the reader already knows.
3. per architecture the result is, per slice, one `(path, line)` per file. an `arch: all` package is cut on the lead architecture only and probed on the rest, so its listing is copied from the lead.

### backends

`--backend=file|builtin`. the default is `file` when a binary is found, else `builtin`. the json names what ran and its version, since descriptions change between libmagic releases.

- `file` -- `$FILE`, else `file` on `PATH`. one process per rootfs: `file -N -0 -f -` with the paths on stdin, every flag before `-f` since `file` acts on `-f` while parsing. output is `<path>\0: <line>` per path. the version is the first line of `file --version`.
- `builtin` -- softmagic's embedded database, loaded once. the stat layer `file` adds before reading bytes is reproduced for what a rootfs holds: `setuid`, `setgid` and `sticky` prefixes, `empty` for a zero-size file, the executable bit handed to the rules (it is what makes a script `executable` in the description), and the text phase's `, ` join after a prefix. the version is softmagic's module version and the `file` release it implements.

the two are checked against each other: a unit test over handmade files, and an e2e test over real debs. a line that differs after normalisation fails the test. the spike that chose softmagic found zero differing lines over 5030 files from eight ubuntu-24.04 rootfs trees on four architectures.

### normalisation

a `file` line is normalised for folding across architectures by removing what the architecture determines by definition and what carries no information, and nothing else. the raw line is kept and shown with `--raw`.

- dropped: `BuildID[<kind>]=<hex>`, `for GNU/Linux <version>`, `version <n> (<abi>)`, text encodings (`ASCII`, `UTF-8`, `Unicode`, `ISO-8859`) so that `Python script, ASCII text executable` and `Python script, Unicode text, UTF-8 text executable` are one line, gzip's `original size modulo 2^32 <n>` and `last modified: <date>`.
- replaced: the elf class, endianness and machine (`64-bit LSB ... x86-64`, `32-bit LSB ... ARM, EABI5`, `64-bit MSB ... IBM S/390`) by `<arch>` when they are the cut's architecture's, and by `<other:<arch>>` when they are another ubuntu architecture's, so a wrong-arch binary is loud; the loader path after `interpreter` by `<ld>` when it is the architecture's own loader, left alone otherwise.
- kept: the kind (`pie executable`, `shared object`, `relocatable`, `current ar archive`, script, text, compressed data, `empty`), the linkage (`statically linked`, `dynamically linked`, `static-pie linked`), `stripped` / `with debug_info, not stripped`, the script's interpreter, a symlink's target, text modifiers (`with very long lines`, `with CRLF line terminators`).

`openssl` on seven architectures becomes `ELF <arch> pie executable, dynamically linked, interpreter <ld>, stripped` everywhere. the armhf loader of ubuntu 24.04 is `with debug_info, not stripped` where every other is `stripped`, and stays apart, which is the point.

the table of per-architecture strings is in one place (`internal/magic/normalise.go`) and the test corpus is real `file` 5.48 lines from every architecture.

### folding

paths are normalised with `chisel-coverage/pkg/fold.Normalise` (the multiarch triplet and the architecture's name become `<arch>` and `<gnu>`), except for an `arch: all` package, whose paths are compared as they are. per package and slice, every `(normalised path, normalised line)` is keyed by the architectures that have it, and each key is a block, most architectures first. an architecture's blocks sum to its listing. architectures that are not available or whose cut failed are reported once per package, not per slice. `--no-group` makes every architecture its own block with raw paths and raw lines.

## cli

```
chisel-file [flags] <package>...
      --release=<dir|name>     chisel-releases checkout, or a release name like
                               ubuntu-24.04 fetched by chisel into its cache
                               (default: .)
      --arch=<a>[,<a>...]      architectures to list; "all" for every one
                               (default: host)
      --slice=<s>[,<s>...]     only these slices, bare or <package>_<slice>;
                               "all" is the whole deb (default: all slices)
      --ignore=<glob>[,...]    leave matching paths out (chisel glob syntax)
      --all                    list the whole deb too, as slice "all"; works
                               without a slice definition file
      --backend=<file|builtin> what runs file: the binary on PATH, or the
                               built-in port (default: file when found)
      --raw                    show file's lines as they are, no normalising;
                               with --json, add every architecture's raw line
      --no-group               one block per architecture, raw paths and lines
      --json                   machine-readable output, folded
      --keep                   keep the temp releases and rootfs dirs
  -j, --jobs=<n>               cuts to run at once; -1 for one per cpu
                               (default: -1)
  -v, --verbose                one line per cut on stderr; twice also streams
                               chisel's output
      --version                print the version and exit
  -h, --help                   this text
```

flags are parsed by go-flags, as in chisel and chisel-coverage. `--all` with a package that has a real slice named `all` is an error. exit codes: 0 ran fine; 2 usage error, io error, no backend, or a cut that failed; 130 interrupted. a path that `file` could not read is a line (`error: <text>`), not a failure. the text report gets the same little colour as chisel-coverage, only on a terminal with `NO_COLOR` unset.

## report

text, one block per (package, slice, group of architectures), nothing indented and no blank lines:

```
openssl 3.0.13-0ubuntu3.4 bins [amd64, arm64, armhf, i386, ppc64el, riscv64, s390x]
/usr/bin/c_rehash: Perl script text executable
/usr/bin/openssl: ELF <arch> pie executable, dynamically linked, interpreter <ld>, stripped
libc6 2.39-0ubuntu8 libs [amd64, arm64, i386, ppc64el, riscv64, s390x]
/usr/lib/<arch>-linux-<gnu>/ld-linux-<arch>.so.1: ELF <arch> shared object, static-pie linked, stripped
libc6 2.39-0ubuntu8 libs [armhf]
/usr/lib/arm-linux-gnueabihf/ld-linux-armhf.so.3: ELF <arch> shared object, static-pie linked, with debug_info, not stripped
bash [i386]: not available
```

a line is `<path>: <line>`, `file`'s own format, sorted by path, so `cut -d: -f2- | sort | uniq -c` is the histogram. a block of one architecture shows its real path, and its line normalised like every other, so that what differs between two blocks is what is left after the noise. `--raw` shows raw lines, which do not fold, so it implies `--no-group` in text.

`--json` is always folded:

```json
{"run": {"backend": "file", "magic": "file-5.48", "chisel": "v1.2.0", "release": "ubuntu-24.04"},
 "packages": [{"name": "openssl",
   "arches": [{"arch": "amd64", "available": true, "version": "3.0.13-0ubuntu3.4", "deb-arch": "amd64", "error": null}, ...],
   "slices": [{"name": "bins", "files": [
     {"path": "/usr/bin/openssl", "arches": ["amd64", "arm64"], "file": "ELF <arch> pie executable, ..."},
     ...]}]}]}
```

a record's `file` is normalised, and its `path` too when it spans several architectures. `--raw` adds `"raw": {"<arch>": "<line>"}` to every record. `arches` holds every architecture asked for, so a reader knows which are missing from a record because the package is not there and which because the file is not.

## layout

mirrors chisel-coverage: `cmd/`, `internal/{cli,listing,magic}`, `version.go` + `VERSION`, `e2e/`, lowercase `makefile`, agpl-3.

- `internal/magic` -- the `Backend` interface, the `file` and `builtin` backends, the normalisation table.
- `internal/listing` -- the run: `cuts.Run` with a `Visit` that lists each rootfs, ignores applied, `arch: all` stand-ins copied, then the folding into report groups.
- `internal/cli` -- flags, text and json rendering, exit codes.

deps: `github.com/lczyk/chisel-coverage/pkg` (`cuts`, `cut`, `cache`, `release`, `sdf`, `fold`), `code.eitanity.com/softmagic`, `github.com/jessevdk/go-flags`, `github.com/lczyk/assert`, `github.com/lczyk/version/go`.

## testing

- unit tests with a fake `chisel` that fills `--root` from the slice name (the fixture chisel-coverage's pool tests use) and a fake `file` that answers from the path, so the listing, ignores, stand-ins and folding are checked without either binary.
- the normalisation table against a checked-in corpus of real `file` 5.48 lines from all seven architectures.
- the builtin backend on handmade files, and against the `file` binary when one is found: the same lines, raw.
- `e2e/` against the real archive and a real chisel-releases clone, behind `-tags e2e` and `CHISEL_FILE_E2E=1`, running both backends over the same cuts and diffing them.

## later, maybe

- `--mime`: `file --mime-type` lines instead of descriptions.
- `--summary`: a histogram per block instead of the paths.
- a local `.deb` as input, through chisel-proxy.
