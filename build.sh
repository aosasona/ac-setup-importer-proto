#!/usr/bin/env bash
# Build DriveKit Setup Importer for Windows (64-bit)
# Run this from any machine with Go installed.

set -euo pipefail

echo "Building drivekit-importer.exe..."
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -H=windowsgui" -o drivekit-importer.exe .
echo "Done → drivekit-importer.exe"
echo ""
echo "Drop the .exe anywhere and double-click to run."
echo "It will open your browser automatically at http://localhost:7432"
