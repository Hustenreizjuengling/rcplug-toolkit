# Development

## Requirements

- **Go 1.22** or newer. Nothing else is needed to build; the toolkit uses only the
  standard library.
- Optional, for the full checks: **shellcheck**, **Wine** (`make test-windows`), and for
  the installer test `start-stop-daemon`, `curl`, `python3` and a checkout of
  [rcplug](https://github.com/Hustenreizjuengling/rcplug) next to this repository.

| Target | Does |
|---|---|
| `make check` | gofmt, vet (also as Windows code), race tests, shellcheck: what CI runs |
| `make test-windows` | flash and setup tests as Windows binaries under Wine |
| `make installer-test` | builds rcplug's device tree for this machine (`RCPLUG=../rcplug`) and installs, updates, extends and uninstalls it with the wizard and `rcplug-setup` in a sandbox |
| `make build` | the tools for this machine in `dist/<os>-<arch>/` |
| `make package` | release files in `dist/`: setup programs for every system, `rcplug-cfw`, `rcplug-flash`, wizard, the Windows zip, `SHA256SUMS` |

The Windows zip includes rcplug's device tarball if `$(RCPLUG)/dist` has one (`make
package` in rcplug), so a Windows user gets everything in one download.

## Repository layout

```
cmd/rcplug-setup/    setup window (web/) and text menu
cmd/rcplug-cfw/      firmware download and key insertion
cmd/rcplug-flash/    flashing over USB
internal/fwdl/       stock firmware download from RØDE (HTTPS, host allowlist, MD5)
internal/fwimage/    the RØDECaster models and identifying an image
internal/cfw/        adding SSH keys to an image without unpacking it
internal/flash/      pre-flight checks and flashing: Linux drives the vendored scripts
                     in scripts/, Windows uses the Go port in native.go + *_windows.go
installer/           wizard, device-side script, Windows text menu
tools/               zip builder, installer test
docs/                documentation
```

## Tests

- **Unit tests** next to the code. Firmware handling is tested against small images
  built in the tests; nothing downloads from RØDE during tests.
- **Flash tests never reach a device.** `internal/flash` runs the Windows flash flow
  against a simulated device and system on every platform; the Windows API layer has
  its own tests, which CI runs on a Windows runner and `make test-windows` under Wine.
- **Installer test** (`tools/installer-test.sh`): a fake `ssh` runs the remote commands
  locally in a sandbox.

## Conventions

- Go standard library only, `gofmt`, `go vet` clean (also with `GOOS=windows`).
- Code, comments and documentation in English. User interfaces in German and English.
- The destructive flash command is only ever sent after an explicit confirmation.
- Shell scripts that run on the device are POSIX sh (bash as `sh` on the Duo) and pass
  shellcheck.

## Releasing

Update `VERSION`, add a section to `CHANGELOG.md`, commit, tag `v<version>` and push
the tag. The release workflow checks the tag against `VERSION`, runs the tests and
publishes the release files.
