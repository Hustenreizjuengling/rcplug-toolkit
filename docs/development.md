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
  locally in a sandbox. `make installer-test` builds rcplug's device tree into
  `dist/rcplug/` (rcplug's own `dist/` stays untouched); the sandbox's dashboard uses
  port 18580 (`RCPLUG_TEST_PORT`).
- **Windows binaries under Wine** (`make test-windows`): a fresh Wine prefix needs
  about 750 MB.

## Conventions

- Go standard library only, `gofmt`, `go vet` clean (also with `GOOS=windows`).
- Code, comments and documentation in English. User interfaces in German and English.
- The destructive flash command is only ever sent after an explicit confirmation.
- Shell scripts that run on the device are POSIX sh (bash as `sh` on the Duo) and pass
  shellcheck.

## Automation on GitHub

| Workflow | When | What |
|---|---|---|
| CI | every push and pull request | `make check` (gofmt, vet also as Windows code, race tests, shellcheck, doc links), the tests with the minimum Go version, the flash and setup tests on a Windows runner, the installer end-to-end test with rcplug (once rcplug is public) and the release files as an artifact |
| Development build | every push to `main` | the pre-release **dev** ("Development build"): the Windows zip with `rcplug-setup.exe` and `rcplug-flash.exe`, the setup programs for macOS and Linux, the firmware tool and the wizard as `<version>-dev.<run>`, replaced by every push. The Windows zip contains rcplug's development build once rcplug is public |
| Release | a tag `v<version>`, or a click | see below |
| Security | every push, pull request and weekly | `govulncheck` (known vulnerabilities in the Go standard library and in code the programs use); CodeQL code scanning, once the repository is public |
| Dependabot | weekly | pull requests for new versions of the GitHub Actions |

Release files are built with the current Go release (`go-version: stable`); a CI job
tests with the minimum version from `go.mod` as well.

## Releasing

Every change describes itself under the newest section of `CHANGELOG.md`,
`## <version> - Unreleased`. Release rcplug first: the Windows zip includes the device package of rcplug's latest release.

**With one click:** *Actions → Release → Run workflow* (or
`gh workflow run release.yml -f version=X.Y.Z`; without a version it takes the one in
`VERSION`). The run dates the changelog's "Unreleased" section as that version, sets
`VERSION`, commits and tags, then builds, publishes the GitHub release
with the changelog section as notes, and finally starts the next patch version's
"Unreleased" section.

**By hand:** set the date and `VERSION` yourself (`tools/release-version.sh prepare
X.Y.Z` does it), commit, tag `v<version>` and push the tag; the release workflow builds,
and publishes.
