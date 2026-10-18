# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/),
versions follow [Semantic Versioning](https://semver.org/) (before 1.0, minor versions
may break compatibility).

## 0.5.0 - Unreleased

First release as its own repository. The tools come from rcplug 0.4.0 (wizard and
device-side script) and everything added since.

### Added
- `rcplug-cfw`: adds your SSH public key to the stock Duo firmware image (`os.tar.gz`)
  for enabling SSH on a device that has none out of the box, preserving order,
  ownership, modes, symlinks, device nodes and extended attributes, with no fakeroot
  and no secrets. Checked against the stock 1.7.6 image: every entry but
  `authorized_keys` is identical. docs/enable-ssh.md describes the whole procedure.
- `rcplug-flash` (step 3, Linux + USB): puts the prepared archive onto the device.
  It detects the RØDECaster on USB (vendor `19f7`), checks `archive.tar.gz` matches
  `archive.md5`, and drives the bundled, verbatim `rodecaster-duo-ssh` flash scripts
  (`internal/flash`, MIT © Guid-Lab). The destructive flash (`--flash`) is gated by a
  confirmation and never sent otherwise; `--detect` and `--print-udev-rule` help set
  up. rcplug keeps the brick-capable HID logic in the proven upstream tool rather
  than reimplementing it. There is no version check on the device's flash path, so
  reflashing the same version or downgrading works.
- The setup GUI flashes too (card 3 "Flash over USB", on Linux): it lists every
  pre-flight check, stages with *Copy only*, and flashes only after you type `flash`
  in a confirmation (also checked by the server); the log streams live, and the
  window will not quit during a run. After *Build image* it fills in the folder.
- Pre-flight checks shared by `rcplug-flash` and the GUI (`flash.Preflight`): besides
  the archive's checksum and the device, the image must be RØDECaster Duo firmware
  (others are refused), an image without an SSH key gets a warning, and HID access,
  the way the update disk gets mounted and Python 3.8+ are checked before anything
  is sent. The run names the checked HID node to the flash scripts.
- Flashing works on Windows too, from the setup GUI (card 3) and `rcplug-flash.exe`,
  both in the Windows zip, with nothing else to install. The bundled scripts need
  Linux, so `internal/flash` carries a Go port of them for Windows: the same `M`/`U`
  commands in report 1, the same `A` ack, the same steps, through the Windows HID,
  SetupAPI and volume APIs (standard library only). It adds what the scripts leave
  out because on Windows the update disk is just the next drive letter: the new
  drive must be FAT, sit on the Duo in update mode (USB `19f7`/`004f`) and have room
  for the archive; other drives are left alone. Files are written through and
  flushed, the drive is dismounted where Windows allows it, and before `U` the HID
  interface is looked up again, as the device comes back in update mode under
  another product ID; `U` goes only to that Duo, on the update disk's USB device. Extra checks: the RØDECaster App must not be running;
  `--drive` names the update disk by hand if the automatic check is wrong. Tested
  with a simulated device in the unit tests and, under Wine, against a virtual Duo
  with the real HID report descriptor; not yet on real hardware.
- Flash pre-flight checks the device's model by its USB product ID (each model and
  mode has its own, from the firmware's gadget script): a RØDECaster Pro II is
  refused, so is an unknown ID (`--node` overrides) or more than one Duo, and a Duo
  still in update mode from an earlier run must be switched off and on first.
  `rcplug-flash` prints the pre-flight warnings.
- CI runs the flash and setup tests on Windows; `make vet` vets the Windows code,
  `make test-windows` runs those tests under Wine. The Windows zip now carries the
  licence texts.
- Device-aware firmware handling (`internal/fwimage`): the RØDECaster models from
  RØDE's app (Duo supported, Pro II download only, Video family not supported) and
  identification of an image by its own hostname, device tree and version. The
  firmware list, every download and `rcplug-cfw` check the image is for the chosen
  device (an image for another model is refused, even with `--force`); `rcplug-cfw`
  also checks an input against its `.md5`. New `--device`, `--devices`, `--inspect`.
  The setup reads the connected device's model and firmware (`rcplug-remote.sh
  device`), shows them, preselects that device and offers exactly its version.
- Firmware download (`internal/fwdl`): `rcplug-cfw --list` / `--download latest|beta|<version>`
  and the setup GUI fetch the stock Duo firmware from RØDE's update server, as RØDE's
  app does, over HTTPS, from RØDE's hosts only, MD5-verified; older versions by number.
- The wizard reports problems the device logged whenever it connects.
- `rcplug-setup` opens a browser-based setup window by default (install/update,
  download the stock firmware and add your SSH key, diagnostics, plugins, SSH key
  setup); the old text menu stays as `rcplug-setup menu` and `rcplug-setup.cmd`.
- `rcplug-cfw` is now a shared package (`internal/cfw`) used by the CLI and the GUI.
- `rcplug-setup`: a menu-driven setup for Windows (.exe), macOS and Linux (install or
  update, diagnostics, plugins, uninstall, SSH key setup), and `rcplug-setup.cmd`, the
  same for Windows without an .exe. All setup tools share the device script
  `rcplug-remote.sh`; the release has a Windows zip with everything in it.

### Changed
- rcplug installs to `/usr/local/rcplug`. The candidate "persistent partitions"
  (`/data`, `/userdata`, …) do not exist on the Duo, which keeps nothing outside its
  root partition and RØDE's own vfat partitions; a firmware update writes a fresh
  root partition. The installer, README and configuration docs now say that a
  firmware update removes rcplug and the SSH key.
- No more password logins in the setup tools: the firmware has
  `PasswordAuthentication no` and a root password set by RØDE. The GUI's "also allow
  password logins" switch is gone (it gave no access); texts no longer promise a
  password prompt.
