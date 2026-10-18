# Firmware and flashing

What the toolkit relies on about the RØDECaster Duo's firmware and its update path:
the image format, how the device takes an update over USB, and the facts behind
`rcplug-cfw` and `rcplug-flash`. Read [enable-ssh.md](enable-ssh.md) for the
procedure itself. Everything here was checked against the stock images 1.7.6 and
1.7.3; the device itself has not been flashed by the authors yet.

## Firmware updates

RØDE ships each Duo firmware as a plain gzipped root filesystem (`os.tar.gz`)
with an `os.md5` sidecar, named by a per-device manifest
(`rcp2-mini-manifest.json`). The image is **not signed or encrypted**, so a
repacked image — e.g. one with your SSH key added — is accepted as-is. This is
what `rcplug-cfw` prepares and `rcplug-flash` writes; see
[enable-ssh.md](enable-ssh.md).

Flashing is driven over the device's USB HID control interface (vendor `19f7`)
with two one-byte commands, each sent as output report 1 and acknowledged by the
device with `A` (`0x41`):

- `M` (`0x4d`) — enter update mode; the device exposes its update partition as a
  USB mass-storage disk, onto which `archive.tar.gz` + `archive.md5` are copied.
- `U` (`0x55`) — flash the staged archive to the inactive partition, then reboot.

There is **no timing-critical window**: the commands go to the already-running
device and each is acked, so flashing is a plain request/response sequence, not
a DFU-style race. The device keeps two partitions (A/B); a bad flash of one
still boots from the other.

What the device does, as its 1.7.6 image shows (`/etc/init.d/uac2_gadget.sh`,
`rc_audio_mixer`, `/usr/bin/rode-update.sh`):

- **HID interface:** one vendor collection (usage page `0xFF00`, usage 1) with
  report 1 out and report 2 in (63 bytes each) for these commands, and reports
  3/4 (255 bytes) for the control protocol (`/etc/init.d/rc-hid-descriptor.bin`).
  Windows therefore wants 256-byte writes for this collection, so rcplug
  zero-pads report 1 to that length; the bytes after the command are zero
  either way. (Under Wine the device received the same 64 bytes the Linux
  scripts send; what Windows' own HID driver puts on the wire is not checked.)
- **On `M`:** the device creates `/Application/update-staging/usb-update.img`
  (560000 sectors, about 273 MiB), formats it FAT (`mkfs.vfat`, no label),
  restarts its USB gadget in *update* mode (only the mass-storage and HID
  functions, another product ID) and serves the image as the medium of its
  removable USB disk. A host sees the device leave and come back, and a new disk.
- **On `U`:** it mounts that image itself and runs `rode-update.sh`, which
  formats the inactive root partition, unpacks `archive.tar.gz` onto it, points
  the boot loader at it (keeping the other partition as the fallback) and
  deletes the archive.

USB product IDs by model and mode, from the same gadget script. They tell the
models apart, and rcplug's flash check uses them:

| Mode | Duo | Pro II |
|---|---|---|
| stereo | `0050` | `0037` |
| multitrack | `0093` | `0092` |
| extended multitrack | `0095` | `0094` |
| extended stereo | `0079` | `0078` |
| sync mode | `008e` | `008d` |
| update (after `M`) | `004f` | `0030` |

### No version check on the flash path

The device's flash path does **no version comparison**: it writes whatever
`archive.tar.gz` is staged, regardless of the version currently installed. So
you can reflash the **same** version over itself (useful when you only want to
add a key and change nothing else), **downgrade** to an older version, or
reflash the same image repeatedly.

Any "you are up to date / this is newer" logic lives only in RØDE's desktop app,
and is purely informational. Static analysis of `RODECasterApp.exe` confirms it:
the app compares the device's reported version (three bytes, major/minor/patch)
with the manifest's `os-version` using a plain numeric comparator that treats
"equal or older than the device" as *up to date* and so simply does not offer an
update — there is no "reinstall anyway", "force" or "downgrade" path anywhere in
the binary, and no signature or key check. The comparison only drives UI strings
(`FW_UP_TO_DATE`, `FW_UPDATE_AVAILABLE`). The flash itself carries no version
argument. `rcplug-flash` (the `rodecaster-duo-ssh` scripts it bundles on
Linux, its port of them on Windows) talks to the HID update interface directly
and so is not subject to any of this.

> The app's own HID update uses a richer command set (a version query and a
> handshake, single uppercase letters acked with `'A'`) than the plain
> `M`/`U` enter-update/flash bytes the bundled flash tool (and rcplug's
> Windows port of it) uses. Both are the
> same 1-byte-command + `'A'`-ack framing; the `M`/`U` path is the one
> independently confirmed working on hardware by `rodecaster-duo-ssh`.
