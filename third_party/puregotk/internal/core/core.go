// package core implements core functionality for the generated files
// this core lib is imported by the generated code
package core

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"codeberg.org/puregotk/purego"
)

func PuregoSafeRegister(fptr interface{}, libs []uintptr, name string) {
	// inua: bound on the first call, not at start. A program calls a few
	// hundred of the tens of thousands of functions bound here; looking
	// each up and making its trampoline at start took about 165 ms. The
	// function is a stub until its first call, which binds it (replacing
	// the stub for later calls) and calls it.
	v := reflect.ValueOf(fptr).Elem()
	var once sync.Once
	var real reflect.Value
	v.Set(reflect.MakeFunc(v.Type(), func(args []reflect.Value) []reflect.Value {
		once.Do(func() {
			for _, lib := range libs {
				if sym, err := purego.Dlsym(lib, name); err == nil {
					fn := reflect.New(v.Type())
					purego.RegisterFunc(fn.Interface(), sym)
					real = fn.Elem()
					v.Set(real)
					return
				}
			}
		})
		if !real.IsValid() {
			panic("puregotk: " + name + " is not in the libraries loaded")
		}
		return real.Call(args)
	}))
}

// paths to where the shared object files should be located
// this is unique per architecture
// Debian/Ubuntu has it split into specific arch folder, Fedora is just /usr/lib64
// Flatpak uses /app/lib for application libraries and runtimes don't vendor `pkg-config` as the fallback
// Homebrew on macOS uses /opt/homebrew/lib (arm64) or /usr/local/lib/ (amd64)
// MacPorts on macOS /opt/local/lib/
// see:
// https://fedora.pkgs.org/38/fedora-x86_64/gtk4-4.10.1-1.fc38.x86_64.rpm.html
// https://fedora.pkgs.org/38/fedora-aarch64/gtk4-4.10.1-1.fc38.aarch64.rpm.html
// https://ubuntu.pkgs.org/23.04/ubuntu-main-amd64/libgtk-4-1_4.10.1+ds-2ubuntu1_amd64.deb.html
// https://ubuntu.pkgs.org/23.04/ubuntu-main-arm64/libgtk-4-1_4.10.1+ds-2ubuntu1_arm64.deb.html
// https://docs.flatpak.org/en/latest/flatpak-builder-command-reference.html (see --libdir)
// https://docs.brew.sh/Installation
// https://trac.macports.org/wiki/FAQ (see "Can I install software in /usr/local while using MacPorts? ¶")
var paths = map[string][]string{
	"linux_amd64":  {"/app/lib/", "/usr/lib/x86_64-linux-gnu/", "/usr/lib64/", "/usr/lib/"},
	"linux_arm64":  {"/app/lib/", "/usr/lib/aarch64-linux-gnu/", "/usr/lib64/", "/usr/lib/"},
	"darwin_arm64": {"/opt/homebrew/lib/", "/opt/local/lib/"},
	"darwin_amd64": {"/usr/local/lib/", "/opt/local/lib/"},
}

// names is a lookup from library names to shared object filenames
// This is populated dynamically via SetSharedLibrary
var names = map[string][]string{}

// pkgConfNames is a lookup from library names to pkg-config library names
// This is populated dynamically via SetPackageName
var pkgConfNames = map[string]string{}

// SetPackageName registers a pkg-config package name for a library.
// This is used by the code generator to set package names from GIR files.
// It won't override existing entries to preserve defaults.
func SetPackageName(libName, pkgName string) {
	if _, exists := pkgConfNames[libName]; !exists && pkgName != "" {
		pkgConfNames[libName] = pkgName
	}
}

// SetSharedLibraries registers shared library names for a library.
// This is used by the code generator to set library names from GIR files.
// It won't override existing entries to preserve defaults.
func SetSharedLibraries(libName string, sharedLibs []string) {
	if _, exists := names[libName]; !exists && len(sharedLibs) > 0 {
		names[libName] = sharedLibs
	}
}

// findSos tries to find all shared objects from a path and a library name
// It does this by mapping the library name to all suitable shared object filenames and then trying some suffixes
func findSos(path string, name string) []string {
	sos := []string{}
	for _, n := range names[name] {
		suffixes := []string{"", ".0", ".1", ".2"}
		fn := filepath.Join(path, n)
		for _, s := range suffixes {
			if _, err := os.Stat(fn + s); err == nil {
				sos = append(sos, fn+s)
			}
		}
	}
	return sos
}

// findPkgConf finds all shared object files with pkg-config
// it does this by running pkg-config --libs-only-L libname
// and then it loops over the directories returned and finds all suitable ones
func findPkgConf(name string) []string {
	cmd := exec.Command("pkg-config", "--libs-only-L", pkgConfNames[name])
	var out, outerr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &outerr
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pkg-config, failed with: %v and stderr: %s\n", err, outerr.String())
		return []string{}
	}
	outs := strings.Split(out.String(), "-L")
	for _, v := range outs {
		c := strings.TrimSpace(v)
		if c == "" {
			continue
		}
		g := findSos(c, name)
		if len(g) > 0 {
			return g
		}
	}
	return []string{}
}

// GetPaths gets all shared object files from a library name
// it does it in the following order
// see if PUREGOTK_LIBNAME_PATH is set (full path to the lib)
// - e.g. PUREGOTK_GTK_PATH
// see if PUREGOTK_LIB_FOLDER is set (root folder where to look for libs)
// go over the hardcoded paths
// find a library name with pkg-config
// panic if failed
// TODO: Hardcore a library shared object with linker -X flag
// This is useful for packaging
// inua: every generated file's init asks for its library's paths; they
// are looked up once.
var (
	pathsMu    sync.Mutex
	pathsFound = map[string][]string{}
)

func GetPaths(name string) []string {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	if p, ok := pathsFound[name]; ok {
		return p
	}
	p := getPaths(name)
	pathsFound[name] = p
	return p
}

func getPaths(name string) []string {
	// try to get from env var
	ev := fmt.Sprintf("PUREGOTK_%s_PATH", name)
	if v := os.Getenv(ev); v != "" {
		return []string{v}
	}

	// Or if a general folder is set where everywhere is located, return that
	ep := os.Getenv("PUREGOTK_LIB_FOLDER")
	if ep != "" {
		g := findSos(ep, name)
		if len(g) == 0 {
			panic(fmt.Sprintf("Could not find lib: %s, at path: %s with env: %s", name, ep, "PUREGOTK_FOLDER"))
		}
		return g
	}

	// fallback to lookup a path if no env var is found
	gp, ok := paths[runtime.GOOS+"_"+runtime.GOARCH]
	if ok {
		// try to loop over paths
		for _, p := range gp {
			g := findSos(p, name)
			if len(g) > 0 {
				return g
			}

		}
	}
	// last effort: pkg-config
	g := findPkgConf(name)
	if len(g) > 0 {
		return g
	}

	panic(fmt.Sprintf("Path for library: %s not found. Please set the path to this library shared object file manually with env variable: %s or PUREGOTK_LIB_FOLDER. Or make sure pkg-config is setup correctly", strings.ToLower(name), ev))
}

// hasSuffix tests whether the string s ends with suffix.
// This function was copied from purego
func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// ByteSlice creates a pointer to a byte slice of C strings
// This function was copied from purego
func ByteSlice(name []string) **byte {
	if name == nil {
		return nil
	}
	res := make([]*byte, len(name)+1)
	for i, v := range name {
		res[i] = CString(v)
	}

	// the last element is NULL terminated for GTK
	res[len(name)] = nil
	return &res[0]
}

// CString converts a go string to *byte that can be passed to C code.
// This function was copied from purego
func CString(name string) *byte {
	if hasSuffix(name, "\x00") {
		return &(*(*[]byte)(unsafe.Pointer(&name)))[0]
	}
	b := make([]byte, len(name)+1)
	copy(b, name)
	return &b[0]
}

// GoStringSlice gets a string slice from a char** array
// This function was copied from purego
func GoStringSlice(c uintptr) []string {
	var ret []string
	for i := 0; ; i++ {
		ptrAddr := c + uintptr(i)*unsafe.Sizeof(uintptr(0))
		addr := *(*unsafe.Pointer)(unsafe.Pointer(&ptrAddr))
		// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
		ptr := *(*uintptr)(addr)
		if ptr == 0 {
			break
		}
		ret = append(ret, GoString(ptr))
	}

	return ret
}

// GoString copies a char* to a Go string.
// This function was copied from purego
func GoString(c uintptr) string {
	// We take the address and then dereference it to trick go vet from creating a possible misuse of unsafe.Pointer
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&c))
	if ptr == nil {
		return ""
	}
	var length int
	for {
		if *(*byte)(unsafe.Add(ptr, uintptr(length))) == '\x00' {
			break
		}
		length++
	}
	return string(unsafe.Slice((*byte)(ptr), length))
}
