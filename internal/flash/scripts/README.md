# Vendored flash scripts

On Linux, `rcplug-flash` and the setup GUI (`rcplug-setup`, step 3) drive
these two scripts, through `internal/flash`, to put a prepared firmware archive
onto a RØDECaster Duo over USB. On Windows they cannot run (they need Linux's
`hidraw`, `/sys/block` and `udisksctl`); there `internal/flash/native.go` is a
Go port of them that keeps their commands, reports and order of steps, and is
kept in step with them by hand. The scripts are **vendored verbatim** from

- **Guid-Lab/rodecaster-duo-ssh** — <https://github.com/Guid-Lab/rodecaster-duo-ssh>
- commit `e403a67263dc93f504878c2c6395247ec49775a5` (2026-10-02)
- MIT License, © 2026 Guid-Lab (see `LICENSE` in this directory)

The underlying research (firmware format, the HID `M`/`U` flash protocol) is by
nicewrld: <https://hhh.hn/rodecaster-duo-fw/>.

| File | Role |
|------|------|
| `rode_hid.py` | HID transport: locate the device (USB vendor `0x19f7`), send the one-byte `M`/`U` commands, read the `A` ack. |
| `flash_cfw.py` | Stage `archive.tar.gz` + `archive.md5` onto the device's update disk and, only when asked, trigger the flash. |
| `70-rode-hidraw.rules` | udev rule that lets your user talk to the HID node without root. |

On Linux, rcplug keeps the brick-capable HID/flash logic **here, in the proven
upstream tool**, rather than reimplementing it. `internal/flash` (Go) only
checks beforehand (device, HID access, staged files, the image's model) and
runs these scripts with the destructive step gated. `build_cfw.py` from
upstream is intentionally **not** vendored: rcplug builds the archive itself
(`internal/cfw`).

Do not edit these files to add behaviour; update from upstream and keep them
verbatim so the checksums stay auditable. If you change the vendored commit,
update the commit hash above and `THIRD_PARTY_NOTICES.md`, and check that the
Windows port (`internal/flash/native.go`) still does what the new scripts do.
