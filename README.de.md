# rcplug-toolkit

**Die Werkzeuge für den Computer zu [rcplug](https://github.com/Hustenreizjuengling/rcplug) und dem
RØDECaster Duo: SSH auf dem Gerät freischalten, rcplug installieren und
aktualisieren, Plugins hinzufügen, Diagnose holen, deinstallieren.**

| Werkzeug | Macht |
|---|---|
| **Setup-Fenster** (`rcplug-setup`) | Alles Folgende in einem Fenster im Browser, für Windows, macOS und Linux |
| **Firmware-Patcher und -Builder** (`rcplug-cfw`) | Lädt die Original-Firmware von RØDE und fügt deinen SSH-Schlüssel ein, damit das Gerät dich hereinlässt |
| **Firmware-Flasher** (`rcplug-flash`) | Bringt die vorbereitete Firmware per USB aufs Gerät, unter Windows und Linux |
| **Installer und Deinstaller** (Setup-Fenster, `rcplug-wizard.sh`) | Installiert, aktualisiert und entfernt rcplug und seine Plugins per SSH |
| **Diagnose** | Holt die Logs und ein Diagnosepaket vom Gerät, auch wenn die Weboberfläche nicht antwortet |

English: [README.md](README.md)

> **Stand: früh und inoffiziell.** Getestet gegen die Original-Firmware des Duo (1.7.6
> und 1.7.3), mit simulierten Geräten und unter Emulation. **Geflasht wurde bisher einmal an
> einem echten Duo** (von Windows aus, 1.7.3 auf 1.7.6), von Linux aus noch nicht.
> Firmware zu verändern geschieht auf eigene Gefahr. rcplug hat nichts mit RØDE zu tun und wird von RØDE nicht unterstützt.

## ⚠ Haftungsausschluss: Nutzung auf eigene Gefahr

rcplug, seine Plugins und Werkzeuge sind **inoffizielle, experimentelle Software** und
werden „wie besehen“ **ohne jede Gewährleistung** bereitgestellt (siehe
[MIT-Lizenz](LICENSE)). Für den SSH-Zugang wird eine veränderte Firmware auf den
RØDECaster geflasht. Das Flashen, das Installieren von Software auf dem Gerät und das
Ändern seiner Einstellungen **kann das Gerät unbrauchbar machen („bricken“)**,
Einstellungen oder Aufnahmen löschen und **die Garantie erlöschen lassen**. RØDE
unterstützt so veränderte Geräte nicht. Alles, was du mit diesem Projekt machst,
geschieht **in deiner eigenen Verantwortung**: Die Autoren haften nicht für Schäden an
deinem Gerät, deinen Daten oder deinen Produktionen. Nicht während einer Sendung
einsetzen, die nicht ausfallen darf.

## Los geht's

1. **Herunterladen** aus den [Releases](../../releases):
   - Windows 10/11: `rcplug-toolkit-<version>-windows.zip`, entpacken.
   - macOS, Linux: `rcplug-setup-<version>-<system>`, dann `chmod +x`.

   Die Vorabversion *Development build* enthält dieselben Dateien aus dem neuesten
   Code, zum Testen.
2. **Setup starten** (unter Windows `rcplug-setup.exe`). Es öffnet eine Seite im
   Browser mit fünf Schritten:
   1. mit dem Gerät verbinden (IP-Adresse und dein SSH-Schlüssel),
   2. Firmware vorbereiten: RØDEs Original-Firmware laden und deinen Schlüssel einfügen,
   3. per USB flashen (nur einmal, für den SSH-Zugang),
   4. rcplug installieren und aktuell halten,
   5. weiteres: Diagnose holen, einen weiteren SSH-Schlüssel hinterlegen, deinstallieren.
3. `http://<geräte-ip>:8080/` öffnen, das ist rcplugs Startseite.

**Der Duo hat ab Werk keinen SSH-Zugang.** Schritt 2 und 3 fügen einmalig deinen
eigenen Schlüssel in die Firmware ein; lies vor dem Flashen
[docs/enable-ssh.md](docs/enable-ssh.md) (Englisch). Das Gerät hat zwei
Firmware-Partitionen, ein fehlerhaftes Abbild lässt die andere also startfähig – es
verändert aber die Firmware.

Downloads mit `SHA256SUMS` prüfen (`sha256sum -c SHA256SUMS --ignore-missing`, unter
Windows `certutil -hashfile <datei> SHA256`).

## Ohne Fenster

```bash
rcplug-cfw --download latest --key ~/.ssh/rodecaster.pub --out out/   # Firmware vorbereiten
rcplug-flash out/                                                    # aufs Gerät kopieren (USB)
rcplug-flash out/ --flash                                            # flashen (fragt vorher)
./rcplug-wizard.sh --host 192.168.1.50 --key ~/.ssh/rodecaster       # rcplug installieren/aktualisieren
./rcplug-wizard.sh --host 192.168.1.50 --plugin ./sonar              # ein Plugin hinzufügen
./rcplug-wizard.sh --host 192.168.1.50 --diagnostics                 # Diagnose holen
./rcplug-wizard.sh --host 192.168.1.50 --uninstall                   # rcplug entfernen
```

Unter Windows ohne .exe bietet `rcplug-setup.cmd` Installieren, Aktualisieren, Plugins,
Diagnose, Schlüssel hinterlegen und Deinstallieren über ein Textmenü mit Windows' eigenem
`ssh` und `tar`; es fragt nach deiner SSH-Schlüsseldatei (Firmware vorbereiten und
flashen kann es nicht).

## Was die Werkzeuge verändern

- **Firmware:** nur deinen SSH-Schlüssel in roots `authorized_keys` (und, wenn du es
  willst, die Passwort-Einstellung des SSH-Servers). Jede andere Datei von RØDEs Abbild
  bleibt Byte für Byte gleich. Der Flash schreibt die inaktive Partition und wechselt
  auf sie.
- **rcplug:** ein Verzeichnis auf dem Gerät (`/usr/local/rcplug`) und sein Startskript;
  Deinstallieren entfernt beides. Ein Firmware-Update entfernt rcplug und deinen
  Schlüssel; danach den Schlüssel wieder einflashen und neu installieren.
- Am Gerät ändert sich nichts, bevor du bestätigst; zum Flashen musst du `flash`
  eintippen.

## Dokumentation (Englisch)

| | |
|---|---|
| [Enabling SSH](docs/enable-ssh.md) | Der ganze Ablauf, seine Risiken, Flashen unter Windows und Linux |
| [Firmware and flashing](docs/firmware.md) | Was die Firmware ist, wie das Gerät ein Update annimmt, Modell- und USB-IDs |
| [Tools reference](docs/tools.md) | Alle Programme und ihre Optionen |
| [Development](docs/development.md) | Bauen, Testen, Aufbau des Repositorys |

Verwandte Repositorys: [rcplug](https://github.com/Hustenreizjuengling/rcplug) (die Plattform auf
dem Gerät) und [rcplug-plugins](https://github.com/Hustenreizjuengling/rcplug-plugins) (Plugins wie
Sonar).

## Lizenz

MIT, siehe [LICENSE](LICENSE). Die mitgelieferten Flash-Skripte sind MIT-lizenzierter
Fremdcode, siehe [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). RØDE und RØDECaster
sind Marken ihres Inhabers und werden hier nur verwendet, um zu beschreiben, womit die
Werkzeuge arbeiten.
