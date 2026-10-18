package cfw

import (
	"errors"
	"strings"
)

// pubKeyTypes are the key types we accept in authorized_keys.
var pubKeyTypes = []string{
	"ssh-ed25519", "ssh-rsa", "ssh-dss",
	"ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521",
	"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-nistp256@openssh.com",
}

// ValidatePublicKey checks, without a crypto dependency, that s is a single-
// line OpenSSH public key (not a private key, not a list).
func ValidatePublicKey(s string) error {
	if s == "" {
		return errors.New("empty")
	}
	if strings.ContainsAny(s, "\r\n") {
		return errors.New("more than one line (is this a private key or a list?)")
	}
	if strings.Contains(s, "PRIVATE KEY") {
		return errors.New("this is a private key; use the .pub file")
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return errors.New("not a 'type base64[ comment]' public key")
	}
	ok := false
	for _, t := range pubKeyTypes {
		if fields[0] == t {
			ok = true
			break
		}
	}
	if !ok {
		return errors.New("unknown key type " + fields[0])
	}
	if len(fields[1]) < 16 || strings.TrimRight(fields[1], "=") == "" {
		return errors.New("key body is not base64")
	}
	for _, c := range fields[1] {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return errors.New("key body is not base64")
		}
	}
	return nil
}
