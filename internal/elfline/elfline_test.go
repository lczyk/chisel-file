package elfline

import (
	"testing"

	"github.com/lczyk/assert"
)

func TestParse(t *testing.T) {
	l, ok := Parse("ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=8a71473f, for GNU/Linux 3.2.0, stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Mode, "")
	assert.Equal(t, l.Class+" "+l.Endian+" "+l.Type+" | "+l.Machine+" | "+l.ABI, "64-bit LSB pie executable | x86-64 | SYSV")
	assert.Equal(t, l.Linkage, "dynamically linked")
	assert.Equal(t, l.Interp, "/lib64/ld-linux-x86-64.so.2")
	assert.Equal(t, l.BuildID, "BuildID[sha1]=8a71473f")
	assert.Equal(t, l.Kernel, "3.2.0")
	assert.Equal(t, l.Stripped, "stripped")
	assert.Len(t, l.Extras, 0)
	assert.Equal(t, l.Arch(), "amd64")

	// armhf has no comma before the abi tag; riscv64 and ppc64el have commas
	// inside the machine.
	l, ok = Parse("setuid, setgid ELF 32-bit LSB shared object, ARM, EABI5 version 1 (SYSV), dynamically linked, BuildID[sha1]=ab, with debug_info, not stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Mode, "setuid, setgid")
	assert.Equal(t, l.Machine, "ARM, EABI5")
	assert.Equal(t, l.Arch(), "armhf")
	assert.Equal(t, l.Type, "shared object")
	assert.EqualArrays(t, l.Extras, []string{"with debug_info"})
	assert.Equal(t, l.Stripped, "not stripped")
	l, ok = Parse("ELF 64-bit LSB pie executable, UCB RISC-V, RVC, double-float ABI, version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-riscv64-lp64d.so.1, for GNU/Linux 4.15.0, stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Arch(), "riscv64")
	assert.Equal(t, l.Kernel, "4.15.0")
	l, ok = Parse("ELF 64-bit LSB shared object, 64-bit PowerPC or cisco 7500, OpenPOWER ELF V2 ABI, version 1 (GNU/Linux), dynamically linked, BuildID[sha1]=ab, stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Arch(), "ppc64el")
	assert.Equal(t, l.ABI, "GNU/Linux")
	assert.Equal(t, l.Interp, "")

	l, ok = Parse("ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, no section header")
	assert.That(t, ok)
	assert.Equal(t, l.Linkage, "statically linked")
	assert.EqualArrays(t, l.Extras, []string{"no section header"})
	assert.Equal(t, l.Stripped, "")
	l, ok = Parse("ELF 64-bit LSB executable, UCB RISC-V, double-float ABI, version 1 (SYSV), statically linked, Go BuildID=abc/def, stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Arch(), "riscv64")
	assert.Equal(t, l.BuildID, "Go BuildID=abc/def")
	l, ok = Parse("ELF 32-bit MSB relocatable, MIPS, MIPS32 version 1 (SYSV), not stripped")
	assert.That(t, ok)
	assert.Equal(t, l.Machine, "MIPS, MIPS32")
	assert.Equal(t, l.Arch(), "")
	l, ok = Parse("ELF 64-bit LSB core file, x86-64, version 1 (SYSV), SVR4-style, from 'sleep 100', real uid: 1000")
	assert.That(t, ok)
	assert.Equal(t, l.Type, "core file")
	assert.EqualArrays(t, l.Extras, []string{"SVR4-style", "from 'sleep 100'", "real uid: 1000"})

	_, ok = Parse("POSIX shell script, ASCII text executable")
	assert.That(t, !ok)
	_, ok = Parse("ELF relocatable thing")
	assert.That(t, !ok)
}

func TestShort(t *testing.T) {
	for _, tc := range []struct{ line, arch, want string }{
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, BuildID[sha1]=8a71, for GNU/Linux 3.2.0, stripped", "amd64",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 32-bit LSB pie executable, ARM, EABI5 version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-armhf.so.3, BuildID[sha1]=0d77, for GNU/Linux 3.2.0, stripped", "armhf",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit MSB shared object, IBM S/390, version 1 (GNU/Linux), dynamically linked, BuildID[sha1]=ab, stripped", "s390x",
			"ELF <arch> lib, dynamic, stripped"},
		{"ELF 32-bit LSB shared object, ARM, EABI5 version 1 (SYSV), static-pie linked, BuildID[sha1]=ab, with debug_info, not stripped", "armhf",
			"ELF <arch> lib, static-pie, with debug_info, not stripped"},
		{"ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, no section header", "amd64",
			"ELF <arch> exec, static, no section header"},
		{"ELF 64-bit LSB relocatable, x86-64, version 1 (SYSV), not stripped", "amd64", "ELF <arch> obj, not stripped"},
		{"setuid ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, stripped", "amd64",
			"setuid ELF <arch> pie, dynamic, <ld>, stripped"},
		// Another architecture's binary is loud and keeps its loader.
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /lib64/ld-linux-x86-64.so.2, stripped", "arm64",
			"ELF <other:amd64> pie, dynamic, /lib64/ld-linux-x86-64.so.2, stripped"},
		// A machine not in the table keeps its words, an unusual loader its path.
		{"ELF 32-bit MSB executable, MIPS, MIPS32 version 1 (SYSV), statically linked, stripped", "amd64",
			"ELF 32-bit MSB exec, MIPS, MIPS32, static, stripped"},
		{"ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV), dynamically linked, interpreter /usr/lib/klibc/bin/klibc.so, stripped", "amd64",
			"ELF <arch> pie, dynamic, /usr/lib/klibc/bin/klibc.so, stripped"},
		{"ELF 64-bit LSB core file, x86-64, version 1 (SYSV), SVR4-style, from 'sleep 100'", "amd64",
			"ELF <arch> core, SVR4-style, from 'sleep 100'"},
		// Older libmagic spellings and per-binary flags after the machine.
		{"ELF 32-bit LSB pie executable, Intel 80386, version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux.so.2, BuildID[sha1]=ab, for GNU/Linux 3.2.0, stripped", "i386",
			"ELF <arch> pie, dynamic, <ld>, stripped"},
		{"ELF 64-bit LSB executable, UCB RISC-V, double-float ABI, version 1 (SYSV), statically linked, Go BuildID=LYWah4EPy9YFm9DKDBW8/nd-9zn_WtuJnc-dT-84d/qU-_qtPPbowc4mSTnAfz/inMnSxNCvCQmHfhNqSdq, stripped", "riscv64",
			"ELF <arch> exec, static, stripped"},
		{"ELF 64-bit LSB shared object, 64-bit PowerPC or cisco 7500, Unspecified or Power ELF V1 ABI, version 1 (SYSV), dynamically linked, stripped", "ppc64el",
			"ELF <arch> lib, dynamic, stripped"},
		{"ELF 32-bit LSB executable, ARM, version 1 (SYSV), statically linked, stripped", "armhf",
			"ELF <arch> exec, static, stripped"},
		{"ELF 32-bit MSB executable, PowerPC or cisco 4500, version 1 (SYSV), statically linked, stripped", "ppc64el",
			"ELF 32-bit MSB exec, PowerPC or cisco 4500, static, stripped"},
	} {
		l, ok := Parse(tc.line)
		assert.That(t, ok, tc.line)
		assert.Equal(t, l.Short(tc.arch), tc.want, tc.line)
	}
}
