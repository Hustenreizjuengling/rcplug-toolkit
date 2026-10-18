# Contributing

Thanks for helping! The toolkit is part of rcplug, a hobby project for people who own a
RØDECaster.

## Reports

- **Bugs:** use the bug report template. Include the toolkit version, your computer's
  system, the device and its firmware, and the output or log of the tool.
- **Never post secrets:** no private keys, no diagnostics bundles with unmasked data,
  no photos of the device's serial number.
- **Security problems:** report them privately, see [SECURITY.md](SECURITY.md).

## Code

1. Read [docs/development.md](docs/development.md); for anything that touches the
   firmware or flashing, also [docs/firmware.md](docs/firmware.md).
2. Keep changes focused, and add tests for new behaviour. Run `make check` before you
   push; CI runs it as well, plus the Windows tests and the installer test.
3. Go standard library only. Code, comments and docs in English; UI texts in English
   **and** German.
4. Changes to firmware handling or flashing need extra care: the destructive flash
   command is only ever sent after an explicit confirmation, the vendored scripts in
   `internal/flash/scripts/` stay verbatim, and the Windows port keeps their commands
   and steps. Explain in the pull request how you verified the change.
5. Describe user-visible changes in `CHANGELOG.md` under an "Unreleased" heading.

## Testing on hardware

Flashing writes the device's firmware. Try a change on a device you can recover (the Duo
keeps two firmware partitions), stage first (`rcplug-flash <dir>` without `--flash`),
and never during a live show.

## Licence

By contributing you agree that your contribution is published under the MIT licence of
this repository.
