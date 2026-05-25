@echo off
REM Build DriveKit Setup Importer (run on Windows with Go installed)
echo Building drivekit-importer.exe...
go build -ldflags="-s -w -H=windowsgui" -o drivekit-importer.exe .
echo Done -- drivekit-importer.exe is ready.
pause
