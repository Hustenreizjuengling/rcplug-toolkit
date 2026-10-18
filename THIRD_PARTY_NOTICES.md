# Third-party notices

The toolkit's programs use only the Go standard library. The one piece of third-party
code it ships is the vendored flash tooling below.

## Vendored flash tooling

`internal/flash/scripts/` contains two Python scripts — `rode_hid.py` and
`flash_cfw.py` — taken verbatim from
[rodecaster-duo-ssh](https://github.com/Guid-Lab/rodecaster-duo-ssh)
(commit `e403a67263dc93f504878c2c6395247ec49775a5`), © 2026 Guid-Lab, MIT
License. On Linux, `rcplug-flash` and the setup GUI extract and run them to
put a firmware archive onto the device over USB; rcplug keeps the brick-capable
HID/flash logic in this proven upstream tool rather than reimplementing it. On
Windows, where they cannot run, `internal/flash/native.go` (with the Windows
calls in `internal/flash/*_windows.go`) is a Go port of these two scripts — the
same commands, reports and steps — and so a derived work under the same MIT
licence and copyright notice. The MIT licence text is in
`internal/flash/scripts/LICENSE`, and provenance in that directory's
`README.md`. The underlying research (firmware format, the HID flash protocol)
is by nicewrld: <https://hhh.hn/rodecaster-duo-fw/>.

## Firmware

The toolkit downloads RØDE's stock firmware from RØDE's update server when you ask it
to; it does not ship or host any firmware.

## Trademarks

RØDE, RØDECaster and RØDECaster Duo are trademarks of Freedman Electronics
Pty Ltd. rcplug is not affiliated with, endorsed by or supported by RØDE.
They are used here only to describe what rcplug works with.
