@echo off
rem rcplug setup for Windows without any .exe of ours: plain cmd with the
rem OpenSSH client and tar that Windows 10 (1809+) and 11 include. Keep
rem rcplug-remote.sh and the release tarball rcplug-<version>-aarch64.tar.gz
rem next to this file. Every step runs rcplug-remote.sh on the device.
setlocal EnableExtensions
chcp 65001 >nul
cd /d "%~dp0"
set "HERE=%~dp0"
set "REMOTE=%HERE%rcplug-remote.sh"
set "SSHOPTS=-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 -o LogLevel=ERROR -o HostKeyAlgorithms=+ssh-rsa -o PubkeyAcceptedKeyTypes=+ssh-rsa"

rem ---- language: German if Windows runs in German, else English
set "LOC="
for /f "tokens=3" %%L in ('reg query "HKCU\Control Panel\International" /v LocaleName 2^>nul ^| findstr LocaleName') do set "LOC=%%L"
if /i "%LOC:~0,2%"=="de" goto :lang_de
set "T_TITLE=rcplug setup"
set "T_NOSSH=The OpenSSH client (ssh) is missing. Install it under Settings - Apps - Optional features - OpenSSH Client."
set "T_NOREMOTE=rcplug-remote.sh is missing next to this file."
set "T_HOST=IP address or name of the RØDECaster: "
set "T_USER=SSH user [root]: "
set "T_CHECK=Checking the SSH connection (enter the device password if asked)..."
set "T_SSHFAIL=Could not log in over SSH. Check the address, the network and the password. After a firmware update the host key may have changed: ssh-keygen -R"
set "T_SSHOK=SSH connection OK."
set "T_STATE=rcplug on the device:"
set "T_MENU1=1  Install or update rcplug"
set "T_MENU2=2  Download diagnostics (logs and device state)"
set "T_MENU3=3  Install or update a plugin from a folder"
set "T_MENU4=4  Uninstall rcplug"
set "T_MENU5=5  Set up SSH key login (no more password prompts)"
set "T_MENU0=0  Exit"
set "T_CHOOSE=Choose: "
set "T_NOTGZ=No rcplug-VERSION-aarch64.tar.gz found next to this file."
set "T_SEND=Sending"
set "T_INSTALL=Installing on the device..."
set "T_DONE=Done. Dashboard:"
set "T_FAILED=That did not work. The messages above say why."
set "T_DIAGSAVED=Saved:"
set "T_DIAGNOTE=Passwords and keys are masked. Review the file before sharing it publicly."
set "T_PDIR=Folder of the plugin: "
set "T_PBAD=The folder needs a plugin.json and a name of a-z, 0-9, - and _."
set "T_CONFIRM=Really remove rcplug and ALL plugin data from the device? Type yes: "
set "T_ABORT=Cancelled."
set "T_KEYGEN=Creating an SSH key for this computer..."
set "T_KEYSEND=Adding the key to the device (one last password prompt)..."
set "T_KEYOK=Done. From now on no password is needed."
set "T_PAUSE=Press a key to continue..."
goto :lang_done
:lang_de
set "T_TITLE=rcplug-Setup"
set "T_NOSSH=Der OpenSSH-Client (ssh) fehlt. Installieren unter Einstellungen - Apps - Optionale Features - OpenSSH-Client."
set "T_NOREMOTE=rcplug-remote.sh fehlt neben dieser Datei."
set "T_HOST=IP-Adresse oder Name des RØDECaster: "
set "T_USER=SSH-Benutzer [root]: "
set "T_CHECK=Prüfe die SSH-Verbindung (falls gefragt, das Passwort des Geräts eingeben)..."
set "T_SSHFAIL=Anmeldung per SSH fehlgeschlagen. Adresse, Netzwerk und Passwort prüfen. Nach einem Firmware-Update kann sich der Host-Key geändert haben: ssh-keygen -R"
set "T_SSHOK=SSH-Verbindung OK."
set "T_STATE=rcplug auf dem Gerät:"
set "T_MENU1=1  rcplug installieren oder aktualisieren"
set "T_MENU2=2  Diagnose herunterladen (Logs und Gerätezustand)"
set "T_MENU3=3  Plugin aus einem Ordner installieren oder aktualisieren"
set "T_MENU4=4  rcplug deinstallieren"
set "T_MENU5=5  Anmeldung per SSH-Schlüssel einrichten (keine Passwortabfragen mehr)"
set "T_MENU0=0  Beenden"
set "T_CHOOSE=Auswahl: "
set "T_NOTGZ=Keine rcplug-VERSION-aarch64.tar.gz neben dieser Datei gefunden."
set "T_SEND=Übertrage"
set "T_INSTALL=Installiere auf dem Gerät..."
set "T_DONE=Fertig. Dashboard:"
set "T_FAILED=Das hat nicht geklappt. Die Meldungen oben sagen warum."
set "T_DIAGSAVED=Gespeichert:"
set "T_DIAGNOTE=Passwörter und Schlüssel sind maskiert. Vor dem öffentlichen Teilen bitte durchsehen."
set "T_PDIR=Ordner des Plugins: "
set "T_PBAD=Der Ordner braucht eine plugin.json und einen Namen aus a-z, 0-9, - und _."
set "T_CONFIRM=rcplug und ALLE Plugin-Daten wirklich vom Gerät entfernen? ja eingeben: "
set "T_ABORT=Abgebrochen."
set "T_KEYGEN=Erzeuge einen SSH-Schlüssel für diesen Rechner..."
set "T_KEYSEND=Hinterlege den Schlüssel auf dem Gerät (ein letztes Mal das Passwort)..."
set "T_KEYOK=Fertig. Ab jetzt ist kein Passwort mehr nötig."
set "T_PAUSE=Weiter mit beliebiger Taste..."
:lang_done
title %T_TITLE%

where ssh >nul 2>&1 || (echo %T_NOSSH% & pause & exit /b 3)
if not exist "%REMOTE%" (echo %T_NOREMOTE% & pause & exit /b 5)

echo.
echo  === %T_TITLE% ===
echo.
set "HOST=%~1"
if "%HOST%"=="" set /p "HOST=%T_HOST%"
if "%HOST%"=="" exit /b 2
set "SSHUSER=root"
set /p "SSHUSER=%T_USER%"
if "%SSHUSER%"=="" set "SSHUSER=root"
set "TARGET=%SSHUSER%@%HOST%"

echo %T_CHECK%
ssh %SSHOPTS% %TARGET% "echo ok" >nul
if errorlevel 1 (echo %T_SSHFAIL% %HOST% & pause & exit /b 4)
echo %T_SSHOK%
call :report

:menu
echo.
echo  %T_MENU1%
echo  %T_MENU2%
echo  %T_MENU3%
echo  %T_MENU4%
echo  %T_MENU5%
echo  %T_MENU0%
echo.
choice /c 123450 /n /m "%T_CHOOSE%"
if errorlevel 6 exit /b 0
if errorlevel 5 goto :keys
if errorlevel 4 goto :uninstall
if errorlevel 3 goto :plugin
if errorlevel 2 goto :diagnostics
goto :install

rem ---- show the host state and problems the device logged
:report
set "STATEFILE=%TEMP%\rcplug-check.txt"
ssh %SSHOPTS% %TARGET% sh -s -- check < "%REMOTE%" > "%STATEFILE%" 2>nul
for %%A in ("%STATEFILE%") do if %%~zA GTR 0 (
  echo %T_STATE%
  type "%STATEFILE%"
)
del "%STATEFILE%" >nul 2>&1
exit /b 0

:install
set "TGZ="
for %%F in ("%HERE%rcplug-*-aarch64.tar.gz") do call :pick_tgz "%%~fF" "%%~nxF"
if not defined TGZ (echo %T_NOTGZ% & goto :menu)
echo %T_SEND% %TGZ% ...
ssh %SSHOPTS% %TARGET% "cat > /tmp/rcplug-payload.tgz" < "%TGZ%"
if errorlevel 1 goto :failed
echo %T_INSTALL%
ssh %SSHOPTS% %TARGET% sh -s -- install /tmp/rcplug-payload.tgz < "%REMOTE%"
if errorlevel 1 goto :failed
echo.
echo %T_DONE% http://%HOST%:8080/
goto :menu

rem The examples tarball matches the pattern too; skip it.
:pick_tgz
echo %~2| findstr /b /c:"rcplug-examples-" >nul && exit /b 0
set "TGZ=%~1"
exit /b 0

:diagnostics
set "OUTFILE=%HERE%rcplug-diagnostics-%HOST%.zip"
ssh %SSHOPTS% %TARGET% sh -s -- diagnostics < "%REMOTE%" > "%OUTFILE%"
if errorlevel 1 goto :failed
rem Without a working rcplugd the device sends the raw logs as tar.gz.
set "MAGIC="
set /p MAGIC=<"%OUTFILE%"
if not "%MAGIC:~0,2%"=="PK" (
  move /y "%OUTFILE%" "%HERE%rcplug-logs-%HOST%.tar.gz" >nul
  set "OUTFILE=%HERE%rcplug-logs-%HOST%.tar.gz"
)
echo %T_DIAGSAVED% %OUTFILE%
echo %T_DIAGNOTE%
goto :menu

:plugin
set "PDIR="
set /p "PDIR=%T_PDIR%"
if "%PDIR%"=="" goto :menu
rem A folder dragged into the window arrives in quotes.
set "PDIR=%PDIR:"=%"
if "%PDIR:~-1%"=="\" set "PDIR=%PDIR:~0,-1%"
if not exist "%PDIR%\plugin.json" (echo %T_PBAD% & goto :menu)
for %%D in ("%PDIR%") do set "PNAME=%%~nxD"
echo %PNAME%| findstr /r /x "[a-z0-9][a-z0-9_-]*" >nul || (echo %T_PBAD% & goto :menu)
set "PTGZ=%TEMP%\rcplug-plugin-%PNAME%.tgz"
tar -C "%PDIR%" -czf "%PTGZ%" .
if errorlevel 1 goto :failed
ssh %SSHOPTS% %TARGET% "cat > /tmp/rcplug-plugin-%PNAME%.tgz" < "%PTGZ%"
if errorlevel 1 goto :failed
del "%PTGZ%" >nul 2>&1
ssh %SSHOPTS% %TARGET% sh -s -- plugin %PNAME% /tmp/rcplug-plugin-%PNAME%.tgz < "%REMOTE%"
if errorlevel 1 goto :failed
goto :menu

:uninstall
set "ANSWER="
set /p "ANSWER=%T_CONFIRM%"
if /i not "%ANSWER%"=="yes" if /i not "%ANSWER%"=="ja" (echo %T_ABORT% & goto :menu)
ssh %SSHOPTS% %TARGET% sh -s -- uninstall < "%REMOTE%"
if errorlevel 1 goto :failed
goto :menu

:keys
set "KEY=%USERPROFILE%\.ssh\id_ed25519"
if not exist "%KEY%.pub" (
  echo %T_KEYGEN%
  if not exist "%USERPROFILE%\.ssh" mkdir "%USERPROFILE%\.ssh"
  ssh-keygen -q -t ed25519 -N "" -f "%KEY%"
  if errorlevel 1 goto :failed
)
echo %T_KEYSEND%
ssh %SSHOPTS% %TARGET% "umask 077; mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys" < "%KEY%.pub"
if errorlevel 1 goto :failed
echo %T_KEYOK%
goto :menu

:failed
echo %T_FAILED%
echo %T_PAUSE%
pause >nul
goto :menu
