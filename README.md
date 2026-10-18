# rcplug-toolkit

**The computer-side tools for [rcplug](https://github.com/Hustenreizjuengling/rcplug) and the
RØDECaster Duo: enable SSH on the device, install and update rcplug, add plugins,
fetch diagnostics, uninstall.**

| Tool | Does |
|---|---|
| **Setup window** (`rcplug-setup`) | Everything below in a window in your browser, for Windows, macOS and Linux |
| **Firmware patcher and builder** (`rcplug-cfw`) | Downloads the stock firmware from RØDE and adds your SSH key, so the device lets you in |
| **Firmware flasher** (`rcplug-flash`) | Puts the prepared firmware onto the device over USB, on Windows and Linux |
| **Installer and uninstaller** (setup window, `rcplug-wizard.sh`) | Installs, updates and removes rcplug and its plugins over SSH |
| **Diagnostics extractor** | Downloads the device's logs and a diagnostics bundle, even when the web interface does not answer |

Deutsch: [README.de.md](README.de.md)

> **Status: early and unofficial.** Everything has been tested against the stock Duo
> firmware images (1.7.6 and 1.7.3), simulated devices and under emulation. **Flashing
> has not been tried on a real device yet.** Modifying firmware is at your own risk.
> rcplug is not affiliated with or endorsed by RØDE.

## ⚠ Disclaimer: use at your own risk

rcplug, its plugins and its tools are **unofficial, experimental software**, provided
"as is" **without any warranty** (see the [MIT license](LICENSE)). Getting SSH onto
the RØDECaster means flashing a modified firmware. Flashing, installing software on the
device and changing its settings **can make the device unusable ("brick" it)**, lose
settings or recordings and **void your warranty**. RØDE does not support devices
changed this way. Whatever you do with this project, you do **on your own
responsibility**: the authors accept no liability for damage to your device, your
data or your productions. Do not use it during a show that must not fail.

## Getting started

1. **Download** the toolkit for your computer from the [releases](../../releases):
   - Windows 10/11: `rcplug-toolkit-<version>-windows.zip`, unpack it.
   - macOS, Linux: `rcplug-setup-<version>-<system>`, then `chmod +x` it.

   The pre-release *Development build* has the same files built from the newest code,
   for testing.
2. **Start the setup** (`rcplug-setup.exe` on Windows). It opens a page in your browser
   with five steps:
   1. connect to the device (its IP address and your SSH key),
   2. prepare the firmware: download RØDE's stock firmware and add your key,
   3. flash it over USB (only once, to get SSH access),
   4. install rcplug and keep it up to date,
   5. more: download diagnostics, add another SSH key, uninstall.
3. Open `http://<device-ip>:8080/` for rcplug's dashboard.

**The Duo has no SSH access out of the box.** Steps 2 and 3 add your own key to the
firmware once; read [docs/enable-ssh.md](docs/enable-ssh.md) before you flash. The
device keeps two firmware partitions, so a bad image still leaves the other one
bootable, but this modifies the firmware.

Check downloads against `SHA256SUMS` (`sha256sum -c SHA256SUMS --ignore-missing`, or
`certutil -hashfile <file> SHA256` on Windows).

## Without the window

```bash
rcplug-cfw --download latest --key ~/.ssh/rodecaster.pub --out out/   # prepare the firmware
rcplug-flash out/                                                    # stage it on the device (USB)
rcplug-flash out/ --flash                                            # flash it (asks first)
./rcplug-wizard.sh --host 192.168.1.50 --key ~/.ssh/rodecaster       # install or update rcplug
./rcplug-wizard.sh --host 192.168.1.50 --plugin ./sonar              # add a plugin
./rcplug-wizard.sh --host 192.168.1.50 --diagnostics                 # download diagnostics
./rcplug-wizard.sh --host 192.168.1.50 --uninstall                   # remove rcplug
```

On Windows without the .exe, `rcplug-setup.cmd` offers install, update, plugins,
diagnostics, adding a key and uninstall from a text menu with Windows' own `ssh` and
`tar`; it asks for your SSH key file (it cannot prepare or flash firmware).

## What the tools change

- **Firmware:** only your SSH key in root's `authorized_keys` (and, if you ask, the SSH
  server's password setting). Every other file of RØDE's image stays byte for byte the
  same. The flash writes the device's inactive partition and switches to it.
- **rcplug:** one directory on the device (`/usr/local/rcplug`) plus its start script;
  uninstalling removes both. A firmware update removes rcplug and your key, so after an
  update you flash your key again and reinstall.
- Nothing on the device changes before you confirm; the flash command needs you to type
  `flash`.

## Documentation

| | |
|---|---|
| [Enabling SSH](docs/enable-ssh.md) | The whole procedure, its risks, flashing on Windows and Linux |
| [Firmware and flashing](docs/firmware.md) | What the firmware is, how the device takes an update, model and USB IDs |
| [Tools reference](docs/tools.md) | All programs and their options |
| [Development](docs/development.md) | Building, testing, repository layout |

Related repositories: [rcplug](https://github.com/Hustenreizjuengling/rcplug) (the platform on the
device) and [rcplug-plugins](https://github.com/Hustenreizjuengling/rcplug-plugins) (plugins such
as Sonar).

## License

MIT, see [LICENSE](LICENSE). The bundled flash scripts are MIT-licensed third-party
code, see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). RØDE and RØDECaster are
trademarks of their owner and are used here only to describe what the tools work with.
