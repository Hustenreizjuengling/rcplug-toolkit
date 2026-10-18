# Enabling SSH on a RØDECaster Duo

rcplug is installed over SSH, but **the RØDECaster Duo has no SSH access out of
the box**: current firmware (1.7.x) runs an SSH server, but accepts neither a
password login nor any key of yours: root's `authorized_keys` is empty in 1.7.6, and in
1.7.3 it holds two keys of RØDE's own. Getting in means adding your own SSH key to the
device's firmware once. rcplug keeps RØDE's keys where they are; its checks count only
the others as yours.

> **This modifies the device firmware. Read the whole page first.**
>
> - It can void your warranty and may conflict with future official updates.
> - It is based on public research by nicewrld
>   (<https://hhh.hn/rodecaster-duo-fw/>) and the `rodecaster-duo-ssh` project.
>   The Duo firmware has **no signature check**, so a repacked image is accepted.
> - The device has two firmware partitions (A/B): a bad flash still boots from
>   the other, which makes this fairly recoverable but not risk-free.
> - rcplug downloads the stock firmware and adds your key. Flashing is a
>   separate, confirmed step on **your own** Windows or Linux computer with the
>   device on USB (setup GUI step 3, or `rcplug-flash`). It follows the
>   `rodecaster-duo-ssh` procedure: on Linux it runs that tool's bundled
>   scripts, on Windows it does the same steps itself. The flash step has not
>   been tested by the rcplug authors on real hardware — proceed deliberately.

This is your decision for your own device. If you would rather not touch the
firmware, you can still run and explore everything against the simulator
(`make run-local`); only installing on the physical device needs this.

## The three steps

```
 1. download the stock firmware     2. add your key     3. flash over USB
    (rcplug-cfw)                       (rcplug-cfw)        (rcplug-flash)
```

The setup GUI (`rcplug-setup`, see the README) does steps 1 and 2 in its
"Prepare firmware" card, including generating a key, and step 3 in its "Flash
over USB" card on Windows and Linux. On the command line, `rcplug-cfw` does
steps 1 and 2 and `rcplug-flash` step 3. Both flash front ends follow the proven
`rodecaster-duo-ssh` procedure (see "How rcplug flashes" below).

### 0. What you need

- `rcplug-setup` or `rcplug-cfw` for your computer (in the release, or
  `go build ./cmd/rcplug-cfw`).
- For flashing: the Duo connected by **USB** to a **Windows 10/11** or
  **Linux** computer (the flash step talks to the device's HID interface and
  copies the archive onto the USB disk it then shows), and `rcplug-setup` or
  `rcplug-flash` for it (in the release, or `go build ./cmd/rcplug-flash`).
  - **Windows:** nothing else to install; `rcplug-setup.exe` and
    `rcplug-flash.exe` are in the Windows zip. Close the RØDECaster App first
    (also in the notification area), as it uses the same USB interface.
    Administrator rights are not needed.
  - **Linux:** `python3` (3.8 or newer), and either `udisksctl` or the ability
    to `sudo mount` (the GUI cannot type a sudo password, so it needs
    `udisksctl` or password-less sudo). rcplug bundles the
    `rodecaster-duo-ssh` flash scripts, so you do not install them separately.
- An SSH key pair. The GUI can make one; by hand:

  ```sh
  ssh-keygen -t ed25519 -f ~/.ssh/rodecaster -C rodecaster
  ```

### 1. Download the stock firmware

RØDE publishes each Duo firmware as a plain gzipped root filesystem
(`os.tar.gz`) with an MD5 sidecar (`os.md5`) on `update.rode.com`; a manifest
(`rcp2-mini-manifest.json`) names the current release. This is what RØDE's own
app downloads. rcplug fetches it over HTTPS, only from RØDE's hosts, and checks
the MD5 before the file gets its final name:

```sh
rcplug-cfw --list                 # what RØDE offers right now
rcplug-cfw --download latest      # → rodecaster-duo-1.7.6-os.tar.gz (+ .md5)
rcplug-cfw --download 1.7.3       # an older release, e.g. the one your device runs
```

Taking the version your device already runs changes the least: the flash then
only adds your key. Only the public channels (stable, beta) are offered; the
manifest also lists RØDE's internal test builds, which rcplug leaves alone.

#### Getting the firmware for the right device

RØDE's app keeps one manifest per model, and rcplug uses the same table
(`rcplug-cfw --devices`):

| Device | `--device` | Manifest | rcplug |
|---|---|---|---|
| RØDECaster Duo | `duo` (default) | `rcp2-mini-manifest.json`, releases in `rcp2-duo/` | supported |
| RØDECaster Pro II | `pro2` | `manifest.json`, releases in `rcp2/` | download only, untested |
| RØDECaster Video, Video S, Video Core | — | `rcv/*` | not supported (encrypted firmware) |

The USB product ID changes with the device's mode (stereo, multitrack, update
…), so it is no way to find the firmware for a device. Every stock image
identifies itself instead: the Duo's has the
hostname `rodecaster-pro-ii-mini` (RØDE's internal name for it), the device tree
`meson-axg-rodecaster-pro-ii-mini.dtb` and its version in
`usr/bin/rcp-os.version`. rcplug checks that at every step (and before flashing
also the device: each model has its own set of product IDs, see
[firmware.md](firmware.md#firmware-updates)):

- **Firmware list:** it must be an RØDECaster manifest (`rcp2-update-manifest`),
  and each release must point into the chosen device's own release path, at the
  version it names.
- **Download:** MD5 against RØDE's `os.md5`, then the image must identify as the
  chosen device and version; otherwise the file is not kept.
- **Adding your key:** the input is checked against its `.md5` if one lies next
  to it, and must identify as the chosen device. An image for another model is
  refused, also with `--force`.
- **Setup GUI:** when connected, it reads the device's own model and firmware
  (like RØDE's app shows them before an update), preselects that device, offers
  exactly that version, and shows for any image you pick which model and
  version it is.

`rcplug-cfw --inspect FILE` prints what an image says about itself.

### 2. Add your key

```sh
rcplug-cfw --in rodecaster-duo-1.7.6-os.tar.gz --key ~/.ssh/rodecaster.pub --out out/
# or both steps at once:
rcplug-cfw --download latest --key ~/.ssh/rodecaster.pub --out out/
```

This writes `out/archive.tar.gz` and `out/archive.md5`, the names the device
expects on its update disk. By default it only adds your public key to root's
`authorized_keys`; it changes nothing else and needs no root or `fakeroot` (it
keeps every entry's ownership, mode and order itself). Checked against the stock
1.7.6 and 1.7.3 images: all 11,735 (11,723) entries keep their order, type, content,
mode, owner, modification time and link target; only `authorized_keys` changes. Five
entries with non-ASCII names lose the access and change times stored in their PAX
records, which nothing on the device reads.

| Flag | Effect |
|---|---|
| `--key FILE` | public key to add (repeatable) |
| `--device ID` | the device the image must be for (`duo`, the default; see `--devices`) |
| `--download WHAT` | fetch the stock image first: `latest`, `beta` or a version |
| `--download-dir DIR` | where `--download` saves it (default: current folder) |
| `--enable-password-auth` | also allow password logins (edits `sshd_config`); off by default. On the Duo this alone gives no access: root's password in the stock image is a hash set by RØDE and not public, and rcplug does not set one — use a key |
| `--md5-format FMT` | contents of `archive.md5`; `{hash}` and `{name}` expand. The default is a bare digest, like RØDE's `os.md5`. |
| `--force` | proceed with an input that has no `/etc/passwd` or names no known model; never with one for another model |
| `--devices`, `--list`, `--inspect FILE` | show the known devices, RØDE's current releases, or what an image is for |

### 3. Flash over USB

Both front ends first run the same checks, without sending anything to the
device: `archive.tar.gz` matches `archive.md5`; the image is RØDECaster Duo
firmware (an image for another model, or one that names no model, is refused)
and whether it carries an SSH key (a stock image without one may be flashed to
go back, with a warning); the device is on USB (vendor `19f7`) and is a Duo by
its product ID: a Pro II or an unknown ID is refused (if a Duo ever shows an ID
rcplug does not know, name it with `--node`), as is more than one Duo; other
RØDE devices are left alone with a warning, but better connect only the Duo; and
a Duo still in update mode from an earlier run must be switched off and on
first.
Then what the system needs:

- **Windows:** the HID interface opens for reading and writing, no RØDE app is
  running, and how the update disk will be found.
- **Linux:** the HID node is readable and writable, the update disk can be
  mounted, and Python 3.8+ is there. The run names that HID node to the flash
  scripts.

**In the setup GUI** (on Windows and Linux), card 3 "Flash over USB" takes the output
folder of step 2 (it is filled in after *Build image*) and shows each check.
*Copy only* stages the archive and stops. *Flash…* opens a confirmation where
you type `flash`; the server refuses a flash request without that word. While a
run is going, the log streams live and the window refuses to quit; there is no
cancel button, because stopping the scripts half-way would only leave the
update disk mounted or the device mid-flash. After a run, check again before
the next one. On macOS the card can check a folder but not flash.

**On the command line**, `rcplug-flash` does the same:

```sh
rcplug-flash --detect        # is the device found on USB? (vendor 19f7)
rcplug-flash out/            # stage archive.tar.gz + archive.md5 onto the device (no flash)
rcplug-flash out/ --flash    # stage, then flash the inactive partition and reboot
```

On Windows the same, in a Command Prompt in the unpacked folder:
`rcplug-flash.exe --detect`, `rcplug-flash.exe out\`, `rcplug-flash.exe out\ --flash`.

Staging copies the archive onto the device's update partition (exposed as a USB
disk when the device enters update mode). `--flash` then sends the command that
overwrites the inactive partition and reboots. **Stage first and look before you
flash.** After staging only, the device stays in update mode; switch it off and
on again before the next run (which stages again and then flashes). `--flash` asks for confirmation (type `flash`) unless you pass `--yes`.

rcplug-flash never sends the flash command on its own: without `--flash` it
stages and stops. It also refuses to proceed if any check above fails, for
example when `archive.tar.gz` does not match `archive.md5`, since the device
would reject that anyway.

**On Windows**, a run goes like this. After `M` the Duo restarts its USB as
an update device (only the update disk and the HID interface; another product
ID). Windows sets it up with its own drivers — the first time this can take a
few seconds — and gives the update disk a drive letter: an unlabelled FAT drive
of about 273 MB. Explorer may open a window for it; leave it or close it, but
do not put files on it. rcplug uses the new drive only if it is FAT and sits on
the Duo in update mode (USB vendor `19f7`, product `004f`); any other drive that
turns up meanwhile is reported and left alone. It copies the files with
write-through and flushes them, then dismounts the drive (that part needs
administrator rights and is skipped without them; the files are already on the
disk). Before `U` it looks up the HID interface again, as the update-mode device
has a new path, and sends `U` only to the Duo in update mode on the same USB
device as the update disk. If the check that the drive belongs to the Duo ever
refuses the right drive, `rcplug-flash.exe out\ --drive E:` names it yourself
(the FAT check stays).

**HID access without root (Linux).** Flashing talks to the device's `hidraw` node.
Either run it as root, or install the bundled udev rule once so your user may
talk to the device (the GUI shows the same commands when access is missing):

```sh
rcplug-flash --print-udev-rule | sudo tee /etc/udev/rules.d/70-rode-hidraw.rules
sudo udevadm control --reload && sudo udevadm trigger
```

(On Linux the standalone `rodecaster-duo-ssh` tool still works —
`flash_cfw.py out/ --flash` does the same thing. rcplug-flash just bundles and
drives it so you do not need a separate checkout.)

### Connect

The device gets a new SSH host key after flashing, so clear any stale entry:

```sh
ssh-keygen -R <device-ip>
ssh -i ~/.ssh/rodecaster root@<device-ip>
```

If that works, install rcplug. The setup tools can use this key: point them at it
with `--key ~/.ssh/rodecaster` (`rcplug-wizard.sh`/`rcplug-setup`), or let the
setup add it to the device for you.

## If something goes wrong

- **The download fails with a checksum mismatch:** the file was damaged on the
  way; download again. rcplug never keeps a file that failed the check.
- **The device boots the old firmware unchanged:** the image was likely rejected.
  Compare `out/archive.md5` with RØDE's `os.md5` (both are a bare digest by
  default) and adjust `--md5-format` if needed.
- **No SSH after a flash that did take:** check that the key went to the right
  home (root is `/home/root` on this device) and that your private key matches.
- **The device misbehaves:** a reboot falls back to the other partition. As a
  last resort, flashing the stock image reverts the change.

## How rcplug flashes (and what it keeps upstream)

The flash step drives the device's USB HID interface and update disk; it cannot
be tested against the real device in CI, and a mistake there is the one part of
this procedure that can really hurt. So rcplug sticks to the procedure the
proven `rodecaster-duo-ssh` tool uses:

- **On Linux** it bundles that tool's scripts (`rode_hid.py`, `flash_cfw.py`)
  verbatim and drives them, from `rcplug-flash` and from the setup GUI alike,
  adding only the checks above and a confirmation gate on the destructive
  command. The HID logic stays in the upstream tool, where it can be audited
  against its source (provenance in `internal/flash/scripts/README.md`).
- **On Windows** those scripts cannot run (they need Linux's `hidraw`,
  `/sys/block` and `udisksctl`). rcplug therefore carries a direct Go port of
  the two scripts (`internal/flash/native.go`, Windows calls in
  `internal/flash/*_windows.go`): the same two commands in report 1
  (zero-padded to the length Windows requires), the same `A` ack on report 2,
  the same order of steps — plus the checks the scripts leave out because on
  Windows the update disk is just the next drive letter (it must be FAT, on the
  Duo in update mode, with room for the archive) and a new lookup of the HID
  interface before `U`, which goes only to that Duo.

The Windows path is tested as far as it can be without hardware: the whole flow
against a simulated device and system in the unit tests (which CI runs on
Windows too), and the Windows build under Wine against a virtual Duo (Linux `uhid`)
with the HID report descriptor from the 1.7.6 firmware, which acks `M`/`U`,
re-enumerates in update mode and serves a FAT update image: the reports reach
it byte for byte as from the Linux scripts, and the archive arrives intact. What
Wine cannot show is how Windows mounts the real update disk.

Steps 1 and 2 — fetching RØDE's published image and offline tar manipulation
with no secrets — are pure Go that rcplug tests directly.

The flash step has **not** been tested by the rcplug authors on real hardware,
on Linux or on Windows; the device is never touched from the build/CI
environment. Credit for the
research and the flash tooling goes to
[nicewrld](https://hhh.hn/rodecaster-duo-fw/) and
[rodecaster-duo-ssh](https://github.com/Guid-Lab/rodecaster-duo-ssh) (MIT).

### Is there a version check? Can I reflash the same version?

The device's flash path performs **no version check**: `rcplug-flash`/
`rodecaster-duo-ssh` send the raw update commands and the device writes whatever
`archive.tar.gz` is staged. So you can flash the **same** version over itself
(e.g. 1.7.3 onto a 1.7.3 device — handy when you only want to add your key),
flash an **older** version, or reflash the same image repeatedly. Any
"you're up to date" logic lives only in RØDE's own app UI, which decides whether
to *offer* an update; this procedure bypasses that entirely. See
[firmware.md](firmware.md#firmware-updates) for the details.
