package main

import (
	"os"
	"strings"
)

var lang = "en"

// setLang picks German or English: the --lang flag, RCPLUG_LANG, the usual
// locale variables, then the system's language (Windows).
func setLang(flagValue string) {
	for _, v := range []string{flagValue, os.Getenv("RCPLUG_LANG"), os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"), systemLang()} {
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "de") {
			lang = "de"
		} else {
			lang = "en"
		}
		return
	}
}

func tr(key string) string {
	if s, ok := texts[lang][key]; ok {
		return s
	}
	return texts["en"][key]
}

var texts = map[string]map[string]string{
	"en": {
		"host": "IP address or name of the RØDECaster: ", "checking": "Checking the SSH connection …",
		"sshOK": "SSH connection OK.", "sshFail": "Could not log in over SSH. Check the address, the network and your SSH key (the Duo accepts no password; see docs/enable-ssh.md).",
		"hostKeyChanged": "The device's SSH host key changed (a firmware update can do that). If you trust it, remove the old key: ssh-keygen -R <address>",
		"noSSH":          "No OpenSSH client (ssh) found. Windows: Settings → Apps → Optional features → OpenSSH Client.", "badHost": "Invalid address or user name.",
		"m1": "1  Install or update rcplug", "m2": "2  Download diagnostics (logs and device state)", "m3": "3  Install or update a plugin from a folder",
		"m4": "4  Uninstall rcplug", "m5": "5  Add this computer's SSH key to the device (uses the login that works now)", "m6": "6  Show the state again", "m0": "0  Exit",
		"choose": "Choose: ", "pluginDir": "Folder of the plugin: ", "state": "rcplug on the device:", "notInstalled": "rcplug is not installed on the device yet.",
		"problems": "   The device logged problems:", "problemsHint": "   Menu 2 downloads everything for a bug report.",
		"noPayload": "No rcplug-<version>-aarch64.tar.gz found next to this program.", "sending": "Sending", "installing": "Installing on the device …",
		"noDiag": "The device returned no diagnostics.", "saved": "Saved:", "diagNote": "Passwords and keys are masked. Review the file before sharing it publicly.",
		"badPlugin":        "The folder needs a plugin.json and a name of a-z, 0-9, - and _.",
		"confirmUninstall": "Really remove rcplug and ALL plugin data from the device? (yes/no): ", "aborted": "Cancelled.",
		"keygen": "Creating an SSH key for this computer …", "keySend": "Adding the key to the device …", "keyOK": "Done. This computer's key works on the device now too (until the next firmware update).",
		"error": "Error:", "pressEnter": "Press Enter to close …",
		"guiServing": "The setup is running in your browser at:", "guiOpenManually": "Could not open the browser automatically; open the address above.",
		"guiQuit":         "Leave this window open while you use it. Close it (Ctrl+C) when you are done.",
		"guiNotConnected": "Not connected to a device yet.", "guiKeyExists": "A key of that name already exists:",
		"deviceLine": "Device:", "unknownModel": "unknown model", "firmwareWord": "firmware",
		"guiFlashBusy":    "A flash is running; wait until it has finished.",
		"guiFlashConfirm": `To flash, type the word "flash" to confirm.`,
	},
	"de": {
		"host": "IP-Adresse oder Name des RØDECaster: ", "checking": "Prüfe die SSH-Verbindung …",
		"sshOK": "SSH-Verbindung OK.", "sshFail": "Anmeldung per SSH fehlgeschlagen. Adresse, Netzwerk und SSH-Schlüssel prüfen (der Duo nimmt kein Passwort an; siehe docs/enable-ssh.md).",
		"hostKeyChanged": "Der SSH-Host-Key des Geräts hat sich geändert (das kann ein Firmware-Update bewirken). Wenn du dem vertraust, den alten Schlüssel entfernen: ssh-keygen -R <adresse>",
		"noSSH":          "Kein OpenSSH-Client (ssh) gefunden. Windows: Einstellungen → Apps → Optionale Features → OpenSSH-Client.", "badHost": "Ungültige Adresse oder ungültiger Benutzername.",
		"m1": "1  rcplug installieren oder aktualisieren", "m2": "2  Diagnose herunterladen (Logs und Gerätezustand)", "m3": "3  Plugin aus einem Ordner installieren oder aktualisieren",
		"m4": "4  rcplug deinstallieren", "m5": "5  SSH-Schlüssel dieses Rechners auf dem Gerät hinterlegen (über die Anmeldung, die jetzt geht)", "m6": "6  Zustand erneut anzeigen", "m0": "0  Beenden",
		"choose": "Auswahl: ", "pluginDir": "Ordner des Plugins: ", "state": "rcplug auf dem Gerät:", "notInstalled": "rcplug ist auf dem Gerät noch nicht installiert.",
		"problems": "   Das Gerät hat Probleme protokolliert:", "problemsHint": "   Menü 2 lädt alles für einen Fehlerbericht herunter.",
		"noPayload": "Keine rcplug-<version>-aarch64.tar.gz neben diesem Programm gefunden.", "sending": "Übertrage", "installing": "Installiere auf dem Gerät …",
		"noDiag": "Das Gerät hat keine Diagnose geliefert.", "saved": "Gespeichert:", "diagNote": "Passwörter und Schlüssel sind maskiert. Vor dem öffentlichen Teilen bitte durchsehen.",
		"badPlugin":        "Der Ordner braucht eine plugin.json und einen Namen aus a-z, 0-9, - und _.",
		"confirmUninstall": "rcplug und ALLE Plugin-Daten wirklich vom Gerät entfernen? (ja/nein): ", "aborted": "Abgebrochen.",
		"keygen": "Erzeuge einen SSH-Schlüssel für diesen Rechner …", "keySend": "Hinterlege den Schlüssel auf dem Gerät …", "keyOK": "Fertig. Der Schlüssel dieses Rechners funktioniert jetzt auch (bis zum nächsten Firmware-Update).",
		"error": "Fehler:", "pressEnter": "Zum Schließen Enter drücken …",
		"guiServing": "Das Setup läuft im Browser unter:", "guiOpenManually": "Browser konnte nicht automatisch geöffnet werden; öffne die Adresse oben.",
		"guiQuit":         "Lass dieses Fenster offen, solange du es nutzt. Zum Beenden schließen (Strg+C).",
		"guiNotConnected": "Noch mit keinem Gerät verbunden.", "guiKeyExists": "Ein Schlüssel mit diesem Namen existiert schon:",
		"deviceLine": "Gerät:", "unknownModel": "unbekanntes Modell", "firmwareWord": "Firmware",
		"guiFlashBusy":    "Ein Flash-Vorgang läuft; bitte warten, bis er fertig ist.",
		"guiFlashConfirm": "Zum Flashen zur Bestätigung das Wort „flash“ eintippen.",
	},
}
