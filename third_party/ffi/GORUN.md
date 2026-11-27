# gorun's copy of github.com/jupiterrider/ffi v0.8.0

gorun uses this copy, through the `replace` in its `go.mod`, instead of the upstream module.
Upstream loads libffi in `init()` and panics when it is missing, so the gorun binary, which only
needs libffi for its optional assistant, couldn't start on a machine without it (Alpine, minimal
containers, Debian 11 and older).

The only changes, each marked `gorun patch`:

- `init.go`: libffi is loaded on first use, by `Ready()`, which returns the error instead of
  panicking. `Load` calls it first, so loading a library reports a missing libffi as an error; the
  functions that call into libffi call it too, and panic as before if it can't be loaded.
- `embed.go`: the embedded libffi (macOS, Windows) is written to the cache folder on first use,
  not at every start.
- `lib_unix.go`, `lib_windows.go`: the loader is `load`; the exported `Load` readies libffi first.

To update, copy the new upstream release here and apply the same changes. The licence is MIT
(`LICENSE`); libffi's own licence is in `assets/libffi/LICENSE`.
