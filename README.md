# DriveKit — Setup Importer

Single-binary web app for importing AC setup zips (e.g. Ed's shared setups).

## Usage

1. Build → double-click `.exe` → browser opens automatically at `http://localhost:7432`
2. Setups folder is auto-detected (`Documents\Assetto Corsa\setups`)
3. Drag one or more `.zip` files onto the drop zone → done

## Zip format expected

The zip must contain at least one `.ld` file named using AC's standard pattern:

```
track_&_car_&_driver_&_stint_N.ld
```

e.g. `ks_vallelunga_&_tatuusfa1_&_E. Cavalli_&_stint_22.ld`

This is what AC writes automatically when you record a session.
All `.ini` files in the zip are treated as setups and copied to:

```
<setups folder>\<car>\<track>\<setup name>.ini
```

Fallback detection order if no `.ld` is present:
1. `GHOST_CAR_name_<car>.ghost` → car name
2. `[CAR] MODEL=` in the `.ini` file itself → car name  
(track cannot be determined without a `.ld` file)

## Build

```bash
# Cross-compile for Windows from Linux/macOS
bash build.sh

# Or on Windows with Go installed
build.bat
```

Requires Go 1.21+. No external dependencies.

## Flags

The `-H=windowsgui` linker flag suppresses the console window on Windows.
Remove it if you want to see log output.
