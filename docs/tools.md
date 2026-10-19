# Tools reference

The toolkit's programs, what they do and their options. All of them run on your
computer; none of them changes anything on the device before you confirm.

| Program | Runs on | Does |
|---|---|---|
| `rcplug-setup` | Windows, macOS, Linux | Setup window in the browser (or a text menu): install, update, uninstall, prepare firmware, flash, diagnostics |
| `rcplug-cfw` | Windows, macOS, Linux | Downloads the stock firmware from RØDE and adds your SSH key (patcher and builder) |
| `rcplug-flash` | Windows, Linux | Puts a prepared firmware onto the device over USB |
| `rcplug-wizard.sh` | anything with Bash and ssh | The same install, update, plugin, diagnostics and uninstall steps as a script |
| `rcplug-remote.sh` | the device | The device side of every install tool; the others push it over SSH, nothing has to be on the device beforehand |

## rcplug-setup

```
rcplug-setup [options] [gui | menu | install | diagnostics | plugin DIR | stop | start | resetaccess | uninstall | keys | status]
```

Without a command it opens the setup window in your browser, with the fields filled in
from `--host`, `--user`, `--key` and `--payload`; `menu` (or `--no-gui`) uses the text
menu instead. Paths may start with `~`. On the command line it exits with status 1 when
something fails (connection, key, command) and 2 without an address. `keys` adds this
computer's SSH key (created if needed) to the device once; running it again changes
nothing. `stop` is the emergency brake: it stops rcplug and all plugins on the device
(until `start` or the next boot), also when the dashboard does not answer; the setup
window has the same as *Stop rcplug* / *Start rcplug*. `resetaccess` removes the
dashboard's password (rcplug 0.6.9 and later), for when it is forgotten and the console
cannot confirm the buttons at the device; in the setup window *Remove the dashboard
password*.

Without a package at hand, `install` fetches rcplug's latest release from GitHub into
the user's cache folder, checked as rcplug checks its own updates: the release's
`release.json` must be signed with rcplug's release key, and the package must have the
size and SHA-256 it names. The setup window's *Latest from GitHub* does the same on
request (also when an older package lies next to the program).

| Option | Meaning |
|---|---|
| `--host` | IP address or name of the device |
| `--user` | SSH user (default `root`) |
| `--key` | SSH private key |
| `--payload` | rcplug release tarball `rcplug-<version>-aarch64.tar.gz` (default: the newest next to the program, otherwise rcplug's latest release from GitHub, see below) |
| `--lang` | `de` or `en` (default: the system language) |
| `--yes` | do not ask for confirmation |
| `--version` | print the version |

## rcplug-cfw

Downloads RØDE's stock firmware and adds your SSH key, see
[enable-ssh.md](enable-ssh.md) and [firmware.md](firmware.md).

| Option | Meaning |
|---|---|
| `--list` | show the versions RØDE currently offers for `--device` |
| `--download WHAT` | fetch the stock image first: `latest`, `beta` or a version such as `1.7.3` |
| `--download-dir DIR` | where `--download` saves it (default: current folder) |
| `--in FILE` | stock image (`os.tar.gz`) to add the key to |
| `--key FILE` | SSH public key to add to root (repeatable) |
| `--out DIR` | output folder for `archive.tar.gz` + `archive.md5` (default `out`) |
| `--device ID` | the model the image must be for (`duo`, the default; see `--devices`) |
| `--devices` | list the RØDECaster models the toolkit knows |
| `--inspect FILE` | show which model and version an image is for |
| `--md5-format FMT` | contents of `archive.md5`; `{hash}` and `{name}` expand (default: a bare digest, like RØDE's `os.md5`) |
| `--enable-password-auth` | also allow password logins (edits `sshd_config`). On the Duo this alone gives no access: root's password is set by RØDE and not public |
| `--force` | continue with an image that names no known model (never with one for another model) |

## rcplug-flash

```
rcplug-flash --detect          find the device on USB
rcplug-flash <dir>             stage <dir>/archive.tar.gz onto the device (no flash)
rcplug-flash <dir> --flash     stage, then flash and reboot (asks first)
```

| Option | Meaning |
|---|---|
| `--flash` | after staging, flash and reboot (overwrites the inactive firmware partition) |
| `--yes` | skip the confirmation for `--flash` |
| `--node` | HID interface to use: hidraw node on Linux, device path on Windows (default: found by USB vendor `19f7`) |
| `--drive` | Windows: drive letter of the update disk, only if the automatic check refuses the right drive |
| `--python` | Linux: Python 3 interpreter for the flash scripts (default `python3`) |
| `--print-udev-rule` | Linux: print the udev rule for HID access without root |
| `--detect` | only look for the device |

## rcplug-wizard.sh

```bash
./rcplug-wizard.sh --host 192.168.1.50 --key ~/.ssh/rodecaster        install or update
./rcplug-wizard.sh --host 192.168.1.50 --plugin ./sonar               add or update one plugin
./rcplug-wizard.sh --host 192.168.1.50 --diagnostics                  download logs and diagnostics
./rcplug-wizard.sh --host 192.168.1.50 --uninstall                    remove everything
```

| Option | Meaning |
|---|---|
| `--host H` | device IP or hostname (asked for if missing) |
| `--user U` | SSH user (default `root`) |
| `--key FILE` | SSH private key. The Duo accepts no password, so this is how you log in |
| `--ask-password` | prompt for an SSH password (needs `sshpass`), for devices that accept one; `RCPLUG_SSH_PASSWORD` works too. Never put a password on the command line |
| `--addr A` | dashboard address on the device (default `:8080`, or that of the existing installation) |
| `--dir D` | install directory on the device (default: the existing installation, else `/usr/local/rcplug`) |
| `--payload P` | device tree or `rcplug-*-aarch64.tar.gz` to install (default: the newest tarball next to the wizard) |
| `--plugin DIR` | install or update one plugin directory into an existing installation (repeatable) |
| `--diagnostics` | download a diagnostics bundle to the current folder; works even when the web interface does not respond (then it saves the raw logs as `.tar.gz`) |
| `--out FILE` | where `--diagnostics` saves the bundle |
| `--uninstall` | stop rcplug and remove it, including plugin data |
| `--yes`, `-y` | do not ask for confirmation |
| `--dry-run` | only check the connection and show what would happen |

The SSH host key is remembered on first use and checked afterwards. A firmware update
gives the device a new host key (`ssh-keygen -R <host>`) and removes rcplug and your SSH
key.
