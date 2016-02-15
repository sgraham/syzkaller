// Copyright 2015 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package main

import (
	"bytes"
	"sort"
	"strconv"
	"text/template"
)

type Arch struct {
	GOARCH           string
	CARCH            []string
	KernelHeaderArch string
	KernelInclude    string
	Numbers          []int
}

var archs = []*Arch{
	{"amd64", []string{"__x86_64__"}, "x86", "asm/unistd.h", nil},
	{"arm64", []string{"__aarch64__"}, "arm64", "asm/unistd.h", nil},
	{"ppc64le", []string{"__ppc64__", "__PPC64__", "__powerpc64__"}, "powerpc", "asm/unistd.h", nil},
}

var syzkalls = map[string]int{
	"syz_open_dev":      1000001,
	"syz_open_pts":      1000002,
	"syz_fuse_mount":    1000003,
	"syz_fuseblk_mount": 1000004,
}

func generateSyscallsNumbers(syscalls []Syscall) {
	for _, arch := range archs {
		fetchSyscallsNumbers(arch, syscalls)
		generateSyscallsNumbersArch(arch, syscalls)
	}
	generateExecutorSyscalls(syscalls)
}

func fetchSyscallsNumbers(arch *Arch, syscalls []Syscall) {
	includes := []string{arch.KernelInclude}
	var vals []string
	defines := make(map[string]string)
	for _, sc := range syscalls {
		name := "__NR_" + sc.CallName
		vals = append(vals, name)
		defines[name] = "-1"
		if nr := syzkalls[sc.CallName]; nr != 0 {
			defines[name] = strconv.Itoa(nr)
		}
	}
	for _, s := range fetchValues(arch.KernelHeaderArch, vals, includes, defines) {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			failf("failed to parse syscall number '%v': %v", s, err)
		}
		arch.Numbers = append(arch.Numbers, int(n))
	}
}

func generateSyscallsNumbersArch(arch *Arch, syscalls []Syscall) {
	buf := new(bytes.Buffer)
	if err := archTempl.Execute(buf, arch); err != nil {
		failf("failed to execute arch template: %v", err)
	}
	writeSource("sys/sys_"+arch.GOARCH+".go", buf.Bytes())
}

func generateExecutorSyscalls(syscalls []Syscall) {
	var data SyscallsData
	for _, arch := range archs {
		var calls []SyscallData
		for i, c := range syscalls {
			calls = append(calls, SyscallData{c.Name, arch.Numbers[i]})
		}
		data.Archs = append(data.Archs, ArchData{arch.CARCH, calls})
	}
	for name, nr := range syzkalls {
		data.FakeCalls = append(data.FakeCalls, SyscallData{name, nr})
	}
	sort.Sort(SyscallArray(data.FakeCalls))

	buf := new(bytes.Buffer)
	if err := syscallsTempl.Execute(buf, data); err != nil {
		failf("failed to execute syscalls template: %v", err)
	}
	writeFile("executor/syscalls.h", buf.Bytes())
}

type SyscallsData struct {
	Archs     []ArchData
	FakeCalls []SyscallData
}

type ArchData struct {
	CARCH []string
	Calls []SyscallData
}

type SyscallData struct {
	Name string
	NR   int
}

type SyscallArray []SyscallData

func (a SyscallArray) Len() int           { return len(a) }
func (a SyscallArray) Less(i, j int) bool { return a[i].Name < a[j].Name }
func (a SyscallArray) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

var archTempl = template.Must(template.New("").Parse(
	`// AUTOGENERATED FILE

// +build {{$.GOARCH}}

package sys

// Maps internal syscall ID onto kernel syscall number.
var numbers = []int{ {{range $nr := $.Numbers}}{{$nr}}, {{end}} }
`))

var syscallsTempl = template.Must(template.New("").Parse(
	`// AUTOGENERATED FILE

{{range $c := $.FakeCalls}}#define __NR_{{$c.Name}}	{{$c.NR}}
{{end}}

struct call_t {
	const char*	name;
	int		sys_nr;
};

{{range $arch := $.Archs}}
#if {{range $cdef := $arch.CARCH}}defined({{$cdef}}) || {{end}}0
call_t syscalls[] = {
{{range $c := $arch.Calls}}	{"{{$c.Name}}", {{$c.NR}}},
{{end}}
};
#endif
{{end}}
`))
