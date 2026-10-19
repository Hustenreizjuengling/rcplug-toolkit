// The web pages' texts in English and German (the choice is shared with
// rcplug's dashboard: localStorage "rcplug-lang").
const T = {
  en: {
    switchLang: 'Deutsch',
    homeTitle: 'rcplug in your browser', homeLead: 'Tools for rcplug and the RØDECaster Duo that run right here: nothing to install, nothing leaves your computer.',
    cardFw: 'Prepare the firmware', cardFwText: 'Add your SSH key to RØDE\'s stock firmware, the first step to rcplug. Uses the toolkit\'s own, tested patcher.',
    cardSetup: 'Setup program', cardSetupText: 'Flash the firmware over USB, install and update rcplug: the toolkit for Windows, macOS and Linux.',
    cardDocs: 'Documentation', cardDocsText: 'How rcplug works, what it changes on the device, and how to write plugins.',
    open: 'Open', downloads: 'Downloads', read: 'Read',
    dlTitle: 'Download the setup program', dlHint: 'Windows: unpack the zip and start rcplug-setup.exe. macOS and Linux: make the file executable (chmod +x) and start it. Check downloads against SHA256SUMS.',
    navFw: 'Firmware', navSetup: 'Setup program', navDocs: 'Documentation', source: 'Source code',
    pathTitle: 'From the stock firmware to rcplug',
    p1: 'Prepare', p1Text: 'Add your SSH key to RØDE\'s firmware, here in the browser.',
    p2: 'Flash', p2Text: 'Put it onto the RØDECaster over USB with the setup program.',
    p3: 'Install', p3Text: 'The setup program installs rcplug over your network.',
    p4: 'Use', p4Text: 'Open rcplug\'s dashboard in any browser on your network.',
    factsTitle: 'Good to know',
    f1: 'The firmware and your key stay on your computer: the page works without a server.',
    f2: 'No cookies, no tracking, no account.',
    f3: 'Open source (MIT): the same patcher as the setup program, built from the same code.',
    f4: 'Unofficial and at your own risk, not made or supported by RØDE.',
    title: 'Prepare the firmware', lead: 'Adds your SSH key to the RØDECaster Duo\'s stock firmware, so you can install rcplug afterwards. Everything happens in this browser tab; the firmware and your key stay on your computer.',
    risk: 'Unofficial and at your own risk: flashing a modified firmware can make the device unusable and voids the warranty. Read “Enable SSH” in the toolkit\'s documentation first.',
    unsupported: 'This browser cannot run the patcher (WebAssembly is missing). Use a current Chrome, Edge, Firefox or Safari, or the setup program.',
    s1: 'RØDE\'s firmware', s1lead: 'Download the stock firmware from RØDE\'s update server, then choose the file here (os.tar.gz, about 170 MB).',
    stable: 'stable', beta: 'beta', download: 'Download from RØDE', listedAt: 'RØDE\'s list as of {at}.', noList: 'The list of RØDE\'s versions is not available; take the file from the setup program\'s download.',
    drop: 'Drop the file here or click to choose it', chosen: '{name} · {mb} MB',
    s2: 'Your SSH key', s2lead: 'The key lets you, and only you, log in to the device. Make a new one here, or paste the public part of one you have.',
    keyNew: 'Make a new key', keyOwn: 'I have one', gen: 'Make a key', genAgain: 'Make another', privDl: 'Private key', pubDl: 'Public key',
    keepKey: 'Save the private key and keep it safe (e.g. in your user folder\'s .ssh): the setup program needs it to install rcplug, and nobody else should have it. On macOS and Linux, ssh wants it readable only by you (chmod 600); the setup program sees to that itself.',
    genReplace: 'The key made before was not saved yet. Make a new one anyway? The firmware then gets the new key.',
    pubPh: 'ssh-ed25519 AAAA… you@computer', pubHint: 'Paste the content of your .pub file: one line.', pubOk: 'The key looks good.', pubBad: 'This is not a usable public key: {err}',
    s3: 'Add the key', s3lead: 'Checks the firmware (model, version, and that the file is whole and one of RØDE\'s current releases) and writes a copy with your key. Takes one to two minutes.',
    go: 'Prepare the firmware', checking: 'Checking the firmware…', writing: 'Writing the prepared copy…', ready: 'Ready.',
    done: 'Done in {s} s: the prepared firmware is ready to download.', kModel: 'Firmware', kInput: 'Stock file', kKey: 'Key added', kOut: 'MD5 of the result',
    inputKnown: 'matches RØDE\'s checksum for {v}', inputUnknown: 'not in RØDE\'s current list (used on your word)',
    unknownAsk: 'The file is whole and a RØDECaster Duo firmware {v}, but not one of the versions RØDE offers now (MD5 {md5}): an older release, or one from elsewhere. Use it only if you know where it comes from.',
    unknownGo: 'Use it anyway', unknownStop: 'Stop', patcherStopped: 'the patcher stopped; try again',
    failed: 'That did not work: {err}',
    s4: 'Onto the device',
    s4lead: 'Flash <b>archive.tar.gz</b> and <b>archive.md5</b> with the toolkit\'s setup program (step “Flash”, over USB) or with <code>rcplug-flash</code>. Afterwards the setup program installs rcplug with your private key. <a href="https://github.com/Hustenreizjuengling/rcplug-toolkit/blob/main/docs/enable-ssh.md" target="_blank" rel="noopener">How flashing works</a>',
    footer: 'rcplug is unofficial software, not affiliated with or endorsed by RØDE Microphones. RØDE and RØDECaster are trademarks of their owner.',
  },
  de: {
    switchLang: 'English',
    homeTitle: 'rcplug im Browser', homeLead: 'Werkzeuge für rcplug und den RØDECaster Duo, die direkt hier laufen: nichts zu installieren, nichts verlässt deinen Rechner.',
    cardFw: 'Firmware vorbereiten', cardFwText: 'Fügt deinen SSH-Schlüssel in RØDEs Original-Firmware ein, der erste Schritt zu rcplug. Mit dem eigenen, getesteten Patcher des Toolkits.',
    cardSetup: 'Setup-Programm', cardSetupText: 'Firmware per USB flashen, rcplug installieren und aktualisieren: das Toolkit für Windows, macOS und Linux.',
    cardDocs: 'Dokumentation', cardDocsText: 'Wie rcplug funktioniert, was es am Gerät ändert und wie man Plugins schreibt.',
    open: 'Öffnen', downloads: 'Downloads', read: 'Lesen',
    dlTitle: 'Setup-Programm herunterladen', dlHint: 'Windows: Zip entpacken und rcplug-setup.exe starten. macOS und Linux: Datei ausführbar machen (chmod +x) und starten. Downloads gegen SHA256SUMS prüfen.',
    navFw: 'Firmware', navSetup: 'Setup-Programm', navDocs: 'Dokumentation', source: 'Quellcode',
    pathTitle: 'Von der Original-Firmware zu rcplug',
    p1: 'Vorbereiten', p1Text: 'Deinen SSH-Schlüssel in RØDEs Firmware einfügen, hier im Browser.',
    p2: 'Flashen', p2Text: 'Sie mit dem Setup-Programm per USB auf den RØDECaster bringen.',
    p3: 'Installieren', p3Text: 'Das Setup-Programm installiert rcplug über dein Netzwerk.',
    p4: 'Benutzen', p4Text: 'Das Dashboard von rcplug in jedem Browser in deinem Netzwerk öffnen.',
    factsTitle: 'Gut zu wissen',
    f1: 'Firmware und Schlüssel bleiben auf deinem Rechner: Die Seite arbeitet ohne Server.',
    f2: 'Keine Cookies, kein Tracking, kein Konto.',
    f3: 'Open Source (MIT): derselbe Patcher wie im Setup-Programm, aus demselben Code gebaut.',
    f4: 'Inoffiziell und auf eigene Gefahr, weder von RØDE gemacht noch unterstützt.',
    title: 'Firmware vorbereiten', lead: 'Fügt deinen SSH-Schlüssel in die Original-Firmware des RØDECaster Duo ein, damit du danach rcplug installieren kannst. Alles passiert in diesem Browser-Tab; Firmware und Schlüssel bleiben auf deinem Rechner.',
    risk: 'Inoffiziell und auf eigene Gefahr: Eine veränderte Firmware zu flashen kann das Gerät unbrauchbar machen und kostet die Garantie. Lies vorher „SSH aktivieren“ in der Dokumentation des Toolkits.',
    unsupported: 'Dieser Browser kann den Patcher nicht ausführen (WebAssembly fehlt). Nimm einen aktuellen Chrome, Edge, Firefox oder Safari – oder das Setup-Programm.',
    s1: 'RØDEs Firmware', s1lead: 'Lade die Original-Firmware von RØDEs Update-Server herunter und wähle die Datei dann hier aus (os.tar.gz, etwa 170 MB).',
    stable: 'stabil', beta: 'Beta', download: 'Bei RØDE herunterladen', listedAt: 'RØDEs Liste, Stand {at}.', noList: 'Die Liste von RØDEs Versionen ist gerade nicht verfügbar; nimm die Datei aus dem Download des Setup-Programms.',
    drop: 'Datei hier ablegen oder klicken und auswählen', chosen: '{name} · {mb} MB',
    s2: 'Dein SSH-Schlüssel', s2lead: 'Mit dem Schlüssel kannst du – und nur du – dich am Gerät anmelden. Erzeuge hier einen neuen oder füge den öffentlichen Teil eines vorhandenen ein.',
    keyNew: 'Neuen Schlüssel erzeugen', keyOwn: 'Ich habe einen', gen: 'Schlüssel erzeugen', genAgain: 'Noch einen erzeugen', privDl: 'Privater Schlüssel', pubDl: 'Öffentlicher Schlüssel',
    keepKey: 'Speichere den privaten Schlüssel und bewahre ihn gut auf (z. B. im Ordner .ssh deines Benutzers): Das Setup-Programm braucht ihn zum Installieren von rcplug, und niemand sonst sollte ihn haben. Unter macOS und Linux will ssh, dass nur du ihn lesen kannst (chmod 600); darum kümmert sich das Setup-Programm selbst.',
    genReplace: 'Der zuvor erzeugte Schlüssel ist noch nicht gespeichert. Trotzdem einen neuen erzeugen? Die Firmware bekommt dann den neuen Schlüssel.',
    pubPh: 'ssh-ed25519 AAAA… du@rechner', pubHint: 'Füge den Inhalt deiner .pub-Datei ein: eine Zeile.', pubOk: 'Der Schlüssel sieht gut aus.', pubBad: 'Das ist kein brauchbarer öffentlicher Schlüssel: {err}',
    s3: 'Schlüssel einfügen', s3lead: 'Prüft die Firmware (Modell, Version, ob die Datei vollständig und eine von RØDEs aktuellen Versionen ist) und schreibt eine Kopie mit deinem Schlüssel. Dauert ein bis zwei Minuten.',
    go: 'Firmware vorbereiten', checking: 'Prüfe die Firmware…', writing: 'Schreibe die vorbereitete Kopie…', ready: 'Fertig.',
    done: 'Fertig in {s} s: Die vorbereitete Firmware liegt zum Herunterladen bereit.', kModel: 'Firmware', kInput: 'Original-Datei', kKey: 'Eingefügter Schlüssel', kOut: 'MD5 des Ergebnisses',
    inputKnown: 'stimmt mit RØDEs Prüfsumme für {v} überein', inputUnknown: 'nicht in RØDEs aktueller Liste (auf deine Entscheidung verwendet)',
    unknownAsk: 'Die Datei ist vollständig und eine Firmware {v} für den RØDECaster Duo, aber keine der Versionen, die RØDE gerade anbietet (MD5 {md5}): ein älteres Release oder eine Datei von anderswo. Verwende sie nur, wenn du weißt, woher sie stammt.',
    unknownGo: 'Trotzdem verwenden', unknownStop: 'Abbrechen', patcherStopped: 'der Patcher ist ausgestiegen; bitte noch einmal versuchen',
    failed: 'Das hat nicht geklappt: {err}',
    s4: 'Aufs Gerät',
    s4lead: 'Flashe <b>archive.tar.gz</b> und <b>archive.md5</b> mit dem Setup-Programm des Toolkits (Schritt „Flashen“, per USB) oder mit <code>rcplug-flash</code>. Danach installiert das Setup-Programm rcplug mit deinem privaten Schlüssel. <a href="https://github.com/Hustenreizjuengling/rcplug-toolkit/blob/main/docs/enable-ssh.md" target="_blank" rel="noopener">So funktioniert das Flashen</a>',
    footer: 'rcplug ist inoffizielle Software und steht in keiner Verbindung zu RØDE Microphones. RØDE und RØDECaster sind Marken ihres Inhabers.',
  },
};
let lang = 'en';
try { lang = localStorage.getItem('rcplug-lang') || ((navigator.language || '').toLowerCase().startsWith('de') ? 'de' : 'en'); } catch (e) { /* ignore */ }
if (!T[lang]) lang = 'en';
document.documentElement.lang = lang;

export const t = (k, p) => (T[lang][k] ?? T.en[k] ?? k).replace(/\{(\w+)\}/g, (m, n) => (p && n in p ? p[n] : m));
export const keys = (l) => Object.keys(T[l]);

export function applyTexts(root = document) {
  root.querySelectorAll('[data-t]').forEach((el) => { el.textContent = t(el.dataset.t); });
  root.querySelectorAll('[data-t-ph]').forEach((el) => { el.placeholder = t(el.dataset.tPh); });
  root.querySelectorAll('[data-t-html]').forEach((el) => { el.innerHTML = t(el.dataset.tHtml); }); // our own texts only
}

export function setupLang() {
  const b = document.querySelector('#lang');
  if (!b) return;
  b.textContent = t('switchLang');
  b.addEventListener('click', () => {
    try { localStorage.setItem('rcplug-lang', lang === 'de' ? 'en' : 'de'); } catch (e) { /* ignore */ }
    location.reload();
  });
}
