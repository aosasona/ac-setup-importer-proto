#!/usr/bin/env bash
# Build DriveKit Setup Importer for Windows (64-bit)
# Run this from any machine with Go installed.

set -euo pipefail

WIN_USER=$(powershell.exe -NoProfile -Command '$env:USERNAME' 2>/dev/null | tr -d '\r')
INSTALL_DIR="/mnt/c/Users/$WIN_USER/AppData/Local/DriveKit"

echo "Building drivekit-importer.exe..."
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -H=windowsgui" -o drivekit-importer.exe .
echo "Done → drivekit-importer.exe"

echo "Installing to \"$INSTALL_DIR\"..."
mkdir -p "$INSTALL_DIR"
cp drivekit-importer.exe "$INSTALL_DIR/drivekit-importer.exe"
echo "Installed → $INSTALL_DIR/drivekit-importer.exe"

echo "Creating shortcuts..."
powershell.exe -NoProfile -Command "
  \$exe  = \"\$env:LOCALAPPDATA\DriveKit\drivekit-importer.exe\"
  \$shell = New-Object -ComObject WScript.Shell

  \$startMenu = \"\$env:APPDATA\Microsoft\Windows\Start Menu\Programs\DriveKit.lnk\"
  \$s = \$shell.CreateShortcut(\$startMenu)
  \$s.TargetPath = \$exe
  \$s.Description = 'DriveKit Setup Importer'
  \$s.Save()

  \$desktop = \"\$env:USERPROFILE\Desktop\DriveKit.lnk\"
  \$s = \$shell.CreateShortcut(\$desktop)
  \$s.TargetPath = \$exe
  \$s.Description = 'DriveKit Setup Importer'
  \$s.Save()
" 2>/dev/null
echo "Shortcuts created (Start Menu + Desktop)"
echo "Tip: right-click the Start Menu entry to pin it to Start or the taskbar"
