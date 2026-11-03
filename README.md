# chisel-file

[![Ubuntu](https://img.shields.io/badge/Ubuntu-E95420?logo=ubuntu&logoColor=white)](https://ubuntu.com)
[![chisel](https://img.shields.io/badge/%F0%9F%94%A8-chisel-E95420)](https://github.com/canonical/chisel)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/lczyk/chisel-file)
![GitHub Tag](https://img.shields.io/github/v/tag/lczyk/chisel-file?filter=v*&sort=semver&label=release)

development tool for [chisel-releases](https://github.com/canonical/chisel-releases) work. for a package, it runs `file` over every path each slice delivers, per architecture, and shows what kind of thing every path is.

## why

when writing or reviewing a slice definition we want to know what actually lands in a slice: binaries where only config was meant, scripts that need an interpreter, a library that is not stripped on one architecture only. `file` answers that, and this runs it on every slice on every arch and folds what agrees.

## use

```shell
chisel-file --release ubuntu-24.04 --arch all --ignore '/usr/share/doc/**' openssl libssl3t64
openssl 3.0.13-0ubuntu3.16 bins [amd64, arm64, armhf, i386, ppc64el, riscv64, s390x]
/usr/bin/c_rehash: Perl script text executable
/usr/bin/openssl: ELF <arch> pie executable, dynamically linked, interpreter <ld>, stripped
openssl 3.0.13-0ubuntu3.16 config [amd64, arm64, armhf, i386, ppc64el, riscv64, s390x]
/etc/ssl/openssl.cnf: text
/usr/lib/ssl/certs: symbolic link to /etc/ssl/certs
/usr/lib/ssl/openssl.cnf: symbolic link to /etc/ssl/openssl.cnf
/usr/lib/ssl/private: symbolic link to /etc/ssl/private
...
libssl3t64 3.0.13-0ubuntu3.16 libs [arm64, armhf, s390x]
/usr/lib/<arch>-linux-<gnu>/engines-3/padlock.so: ELF <arch> shared object, static-pie linked, stripped
libssl3t64 3.0.13-0ubuntu3.16 libs [amd64]
/usr/lib/x86_64-linux-gnu/engines-3/padlock.so: ELF <arch> shared object, dynamically linked, stripped
```

every file and symlink of a slice counts, directories do not. one block per package, slice and set of architectures that agree, most architectures first. a line is `<path>: <description>`, `file`'s own format, so `cut -d: -f2- | sort | uniq -c` is the histogram. descriptions are normalised for folding: build ids, kernel versions, abi tags and text encodings go, the elf machine becomes `<arch>` (or `<other:amd64>` for a binary of the wrong architecture) and the loader `<ld>`, and paths fold like chisel-coverage's. what is kept is what a reviewer reads: the kind, the linkage, stripped or not, the script's interpreter, a symlink's target. `--raw` shows `file`'s lines as they are, `--no-group` one block per architecture.

`--all` lists the whole deb as a slice called `all`, and works for a package that has no slice definition file yet. `--slice all` lists only that. `--ignore` takes chisel globs, as in chisel-coverage.

`--json` is the same folding, machine-readable: per package the state of every architecture, then per slice one record per `(path, description, architectures)`; `--raw` adds every architecture's own line to each record. the `run` block names the backend, its libmagic version, chisel's version and the release.

## backends

`file` is what the host has on `PATH` (or `FILE=/path/to/file`), one process per cut. `builtin` is [softmagic](https://github.com/eitanity/softmagic), a pure-go port of libmagic with its database embedded, which answers byte for byte what `file` 5.48 does. the default is `file` when found, else `builtin`; `--backend` forces one. descriptions change between libmagic releases, so the json says which ran.

## how it works ?

chisel does the cutting through [chisel-coverage/pkg](https://github.com/lczyk/chisel-coverage): a throwaway release with the package's slices stripped of essentials, mutate scripts and generated content, one `cut` per slice per architecture, cuts in parallel with private views of chisel's cache. while a cut's rootfs exists, the backend describes its regular files and the tool reads its symlinks itself (`file` would resolve a symlink's target against the host, not the rootfs). `chisel` is found on `PATH`, or set `CHISEL=/path/to/chisel`.

## build

```shell
make build      # ./bin/chisel-file
make test       # unit tests; the file-vs-builtin one skips without `file`
make e2e        # real chisel against the real archive and a chisel-releases clone, both backends (needs network)
```
