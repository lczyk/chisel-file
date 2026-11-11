# chisel-file

[![Ubuntu](https://img.shields.io/badge/Ubuntu-E95420?logo=ubuntu&logoColor=white)](https://ubuntu.com)
[![chisel](https://img.shields.io/badge/%F0%9F%94%A8-chisel-E95420)](https://github.com/canonical/chisel)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/lczyk/chisel-file)
![GitHub Tag](https://img.shields.io/github/v/tag/lczyk/chisel-file?filter=v*&sort=semver&label=release)

development tool for [chisel-releases](https://github.com/canonical/chisel-releases) work. for a package, it runs `file` over every path each slice delivers, per architecture, and shows what kind of thing every path is.

## why

when writing or reviewing a package slices file we want to know what actually lands in a slice: binaries where only config was meant, scripts that need an interpreter, a library that is not stripped on one architecture only. `file` answers that, and this runs it on every slice on every arch and folds what agrees.

## use

```shell
chisel-file --release ubuntu-24.04 --arch all --slice all --ignore '/usr/share/doc/**' openssl libssl3t64
openssl 3.0.13-0ubuntu3.16 bins [amd64, arm64, armhf, i386, ppc64el, riscv64, s390x]
/usr/bin/c_rehash: Perl script, text executable
/usr/bin/openssl: ELF <arch> pie, dynamic, <ld>, stripped
openssl 3.0.13-0ubuntu3.16 config [amd64, arm64, armhf, i386, ppc64el, riscv64, s390x]
/etc/ssl/openssl.cnf: text
/usr/lib/ssl/certs -> /etc/ssl/certs
/usr/lib/ssl/openssl.cnf -> /etc/ssl/openssl.cnf
/usr/lib/ssl/private -> /etc/ssl/private
...
libssl3t64 3.0.13-0ubuntu3.16 libs [arm64, armhf, s390x]
/usr/lib/<arch>-linux-<gnu>/engines-3/padlock.so: ELF <arch> lib, static-pie, stripped
libssl3t64 3.0.13-0ubuntu3.16 libs [amd64]
/usr/lib/x86_64-linux-gnu/engines-3/padlock.so: ELF <arch> lib, dynamic, stripped
```

every file and symlink of a slice counts, directories do not. one block per package, slice and set of architectures that agree, most architectures first. a line is `<path>: <description>`, `file`'s own format, so `cut -d: -f2- | sort | uniq -c` is the histogram. descriptions are normalised for folding: an elf line becomes `ELF <arch> <type>, <linkage>, <ld>, stripped` with the type one of `exec` / `pie` / `lib` / `obj` / `core` and the linkage `static` / `dynamic` / `static-pie` (`<other:amd64>` for a binary of the wrong architecture, the loader's path when it is not the architecture's own), build ids, kernel versions, abi tags and text encodings go, a language `file` guessed from keywords alone (`c program text`, `Java source`, `HTML document text`, `Nim source code` for the same `.go` file) is plain `text`, as is a shebang `file` could not read (`a bash" script`), a kind said twice is said once, a .net ready-to-run or efi image is `PE <arch> ...` with the machine named by the architecture, klibc's hashed loader is `/usr/lib/klibc-<hash>.so`, phrasing that moved between libmagic releases is written one way, and paths fold like chisel-coverage's. a description folds on its raw form too, so a symlink target that is the same everywhere is shown as it is. what is kept is what a reviewer reads: the kind, the linkage, stripped or not, the script's interpreter, a symlink's target. `--raw` shows `file`'s lines as they are, `--no-group` one block per architecture.

by default the whole deb is listed, as a slice called `deb`, which needs no package slices file. a package the release has no package slices file for is a package with no slices: its deb lists like any other, `--ignore` and all, and `--slice all` finds nothing of it. `--slice` picks what to list: `all` is every slice of the package slices file, `deb` the whole deb, any other name that slice, and they combine (`--slice all,deb`, `--slice bins,deb`). `--ignore` takes chisel globs, as in chisel-coverage. a package can also be given as the path of its file (`slices/bash.yaml`), so tab completion works.

`--json` is the same folding, machine-readable: per package the state of every architecture, then per slice one record per `(path, description, architectures)`; `--raw` adds every architecture's own line to each record, `--no-group` makes every record one architecture's own path and line. the `run` block names the backend, its libmagic version, chisel's version and the release.

## backends

`builtin`, the default, is [softmagic](https://github.com/eitanity/softmagic), a pure-go port of libmagic with its database embedded, which answers byte for byte what `file` 5.48 does, the same on every host. `--backend=path` runs the `file` the host has on `PATH` (or `FILE=/path/to/file`) instead, one process per cut. descriptions change between libmagic releases, so the json says which ran.

## how it works ?

chisel does the cutting through [chisel-coverage/pkg](https://github.com/lczyk/chisel-coverage): a throwaway release with the package's slices stripped of essentials, mutate scripts and generated content, one `cut` per slice per architecture, cuts in parallel with private views of chisel's cache. while a cut's rootfs exists, the backend describes its regular files and the tool reads its symlinks itself (`file` would resolve a symlink's target against the host, not the rootfs). `chisel` is found on `PATH`, or set `CHISEL=/path/to/chisel`.

## build

```shell
make build      # ./bin/chisel-file
make test       # unit tests; the path-vs-builtin one skips without `file`
make e2e        # real chisel against the real archive and a chisel-releases clone, both backends (needs network)
```
