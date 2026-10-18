# Security policy

The toolkit handles SSH keys, logs in to the device as root and writes its firmware, so
security reports are taken seriously.

## Reporting a vulnerability

Please **do not** open a public issue. Use GitHub's private vulnerability reporting
(the repository's *Security* tab → *Report a vulnerability*). Include what an attacker
needs, what they can achieve, and how to reproduce it. You will get an answer within
two weeks. If private reporting is not available, open an issue that only asks for a
private contact, without any details.

## Supported versions

Only the latest release receives fixes.

## Scope

In scope: the programs and scripts in this repository, for example ways to make the
setup window act for another web page, to get a key or a password out of the tools, to
make them install or flash something other than what you chose, or to send the flash
command without your confirmation.

Not in scope: the device's own security (SSH, firmware), which is RØDE's
responsibility, and the platform on the device, which has its own repository
(rcplug) with the same policy.
