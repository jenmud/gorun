//go:build ((freebsd || linux || windows || darwin) && (amd64 || arm64)) || (linux && riscv64)

package ffi

import (
	"runtime"
	"sync"
)

// filename is the name or path to the libffi shared library.
var filename string

// prepare, when set, readies the libffi embedded for this platform before it is loaded.
var prepare func()

// libffiOnce loads libffi the first time it is needed, and libffiErr is why it couldn't be.
// gorun patch: the upstream package loads it in init() and panics when it is missing, which
// stopped every program importing it from starting on a machine without libffi.
var (
	libffiOnce sync.Once
	libffiErr  error
)

// Ready loads libffi, if it isn't loaded yet, and reports why it can't be used, if it can't.
// Load and the functions that call into libffi call it first.
func Ready() error {
	libffiOnce.Do(func() { libffiErr = loadLibffi() })
	return libffiErr
}

// mustReady is Ready for the functions that have no error to return. Load calls Ready first, so
// a program that loaded its library already knows libffi works.
func mustReady() {
	if err := Ready(); err != nil {
		panic(err)
	}
}

func loadLibffi() error {
	// the embedded copy (macOS, Windows) comes first, as upstream's init order made it
	if prepare != nil {
		prepare()
	}
	if len(filename) == 0 {
		switch runtime.GOOS {
		case "freebsd", "linux":
			filename = "libffi.so.8"
		case "windows":
			filename = "libffi-8.dll"
		case "darwin":
			filename = "libffi.8.dylib"
		}
	}

	libffi, err := load(filename)
	if err != nil {
		return err
	}

	prepCif, err = libffi.Get("ffi_prep_cif")
	if err != nil {
		return err
	}

	prepCifVar, err = libffi.Get("ffi_prep_cif_var")
	if err != nil {
		return err
	}

	call, err = libffi.Get("ffi_call")
	if err != nil {
		return err
	}

	closureAlloc, err = libffi.Get("ffi_closure_alloc")
	if err != nil {
		return err
	}

	closureFree, err = libffi.Get("ffi_closure_free")
	if err != nil {
		return err
	}

	prepClosureLoc, err = libffi.Get("ffi_prep_closure_loc")
	if err != nil {
		return err
	}

	getStructOffsets, err = libffi.Get("ffi_get_struct_offsets")
	if err != nil {
		return err
	}

	// Because ffi_get_version and ffi_get_version_number just exist since libffi 3.5.0, we don't panic here.
	getVersion, _ = libffi.Get("ffi_get_version")

	getVersionNumber, _ = libffi.Get("ffi_get_version_number")
	return nil
}
