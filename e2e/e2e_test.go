//go:build e2e

// Package e2e runs the tool for real: a real chisel against the real archive
// and a real chisel-releases clone, with both backends. Gated behind the e2e
// build tag and CHISEL_FILE_E2E because it needs the chisel binary and
// network access.
package e2e

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lczyk/chisel-file/internal/cli"
	"github.com/lczyk/chisel-file/internal/magic"
)

const releaseBranch = "ubuntu-24.04"

var implemented = regexp.MustCompile(`\(file ([0-9.]+)\)`)

func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("CHISEL_FILE_E2E") != "1" {
		t.Skip("set CHISEL_FILE_E2E=1 to run (needs chisel + network)")
	}
	chisel := os.Getenv("CHISEL")
	if chisel == "" {
		chisel = "chisel"
	}
	if _, err := exec.LookPath(chisel); err != nil {
		t.Skipf("chisel binary not found (%v); set CHISEL=/path/to/chisel", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
}

// cloneRelease shallow-clones a real chisel-releases checkout under dir.
func cloneRelease(t *testing.T, dir string) string {
	t.Helper()
	release := filepath.Join(dir, "releases")
	clone := exec.Command("git", "clone", "--quiet", "--depth", "1", "-b", releaseBranch,
		"https://github.com/canonical/chisel-releases", release)
	clone.Stdout, clone.Stderr = os.Stderr, os.Stderr
	if err := clone.Run(); err != nil {
		t.Fatalf("clone chisel-releases: %v", err)
	}
	return release
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what
// it wrote.
func captureStdout(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	code := fn()
	os.Stdout = orig
	_ = w.Close()
	return code, <-done
}

type report struct {
	Run struct {
		Backend string `json:"backend"`
		Magic   string `json:"magic"`
	} `json:"run"`
	Packages []struct {
		Name   string `json:"name"`
		Arches []struct {
			Arch      string  `json:"arch"`
			Available bool    `json:"available"`
			Version   *string `json:"version"`
			Error     *string `json:"error"`
		} `json:"arches"`
		Slices []struct {
			Name  string `json:"name"`
			Files []struct {
				Path   string            `json:"path"`
				Arches []string          `json:"arches"`
				File   string            `json:"file"`
				Raw    map[string]string `json:"raw"`
			} `json:"files"`
		} `json:"slices"`
	} `json:"packages"`
}

func run(t *testing.T, args ...string) report {
	t.Helper()
	code, out := captureStdout(t, func() int { return cli.Main(args) })
	if code != 0 {
		t.Fatalf("chisel-file exited %d:\n%s", code, out)
	}
	var rep report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	return rep
}

// TestOpensslBothBackends lists openssl and libssl3t64 on two architectures
// with the builtin backend, and with the file binary when there is one, and
// wants the same raw lines from both whenever the file binary is the release
// the builtin implements.
func TestOpensslBothBackends(t *testing.T) {
	requireE2E(t)
	release := cloneRelease(t, t.TempDir())
	args := []string{"--release", release, "--arch", "amd64,arm64", "--json", "--raw", "--ignore", "/usr/share/doc/**", "openssl", "libssl3t64"}

	builtin := run(t, append([]string{"--backend", "builtin"}, args...)...)
	if builtin.Run.Backend != "builtin" || !strings.Contains(builtin.Run.Magic, "file 5.") {
		t.Fatalf("unexpected run block: %+v", builtin.Run)
	}
	if len(builtin.Packages) != 2 {
		t.Fatalf("expected two packages, got %d", len(builtin.Packages))
	}
	for _, p := range builtin.Packages {
		for _, a := range p.Arches {
			if !a.Available || a.Error != nil || a.Version == nil {
				t.Fatalf("%s on %s not listed cleanly: %+v", p.Name, a.Arch, a)
			}
		}
	}
	var found, folded int
	for _, s := range builtin.Packages[0].Slices {
		for _, f := range s.Files {
			if f.Path == "/usr/bin/openssl" && s.Name == "bins" {
				found++
				if f.File != "ELF <arch> pie executable, dynamically linked, interpreter <ld>, stripped" {
					t.Errorf("openssl: %s", f.File)
				}
				if len(f.Arches) != 2 || len(f.Raw) != 2 {
					t.Errorf("openssl not folded over both architectures: %+v", f)
				}
			}
			if len(f.Arches) == 2 {
				folded++
			}
		}
	}
	if found != 1 {
		t.Fatalf("/usr/bin/openssl listed %d times in bins", found)
	}
	if folded < 5 {
		t.Fatalf("only %d files folded over both architectures", folded)
	}
	if _, err := os.Stat(filepath.Join(release, "slices", "openssl.yaml")); err != nil {
		t.Fatalf("the checkout was modified: %v", err)
	}

	if _, err := exec.LookPath(magic.FileBinary()); err != nil {
		t.Logf("no file binary (%v): the file backend is not compared", err)
		return
	}
	file := run(t, append([]string{"--backend", "file"}, args...)...)
	// "softmagic v0.4.3 (file 5.48)" against "file-5.48".
	same := strings.HasPrefix(file.Run.Magic, "file-"+implemented.FindStringSubmatch(builtin.Run.Magic)[1])
	raw := map[string]string{}
	for pi, p := range builtin.Packages {
		for si, s := range p.Slices {
			for _, f := range s.Files {
				for arch, line := range f.Raw {
					raw[p.Name+" "+s.Name+" "+arch+" "+f.Path] = line
				}
			}
			for _, f := range file.Packages[pi].Slices[si].Files {
				for arch, line := range f.Raw {
					key := p.Name + " " + s.Name + " " + arch + " " + f.Path
					want, ok := raw[key]
					if !ok {
						t.Errorf("%s: listed by file, not by builtin", key)
						continue
					}
					if want == line {
						continue
					}
					if same {
						t.Errorf("%s:\n  file:    %s\n  builtin: %s", key, line, want)
					} else {
						t.Logf("%s (%s differs from the builtin's release):\n  file:    %s\n  builtin: %s", key, file.Run.Magic, line, want)
					}
				}
			}
		}
	}
}
