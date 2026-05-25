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
