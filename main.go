package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

type ImportResult struct {
	ZipName     string   `json:"zipName"`
	Car         string   `json:"car"`
	Track       string   `json:"track"`
	Files       []string `json:"files"`
	Destination string   `json:"destination"`
	Skipped     []string `json:"skipped,omitempty"`
	Error       string   `json:"error,omitempty"`
	Success     bool     `json:"success"`
}

type DetectResult struct {
	Folder string `json:"folder"`
	Found  bool   `json:"found"`
}

// ---------------------------------------------------------------------------
// Setups folder detection
// ---------------------------------------------------------------------------

func detectSetupsFolder() DetectResult {
	var candidates []string

	// Windows via USERPROFILE
	if up := os.Getenv("USERPROFILE"); up != "" {
		candidates = append(candidates,
			filepath.Join(up, "Documents", "Assetto Corsa", "setups"),
			filepath.Join(up, "OneDrive", "Documents", "Assetto Corsa", "setups"),
			filepath.Join(up, "OneDrive - Personal", "Documents", "Assetto Corsa", "setups"),
		)
	}

	// Generic home dir (macOS / Linux)
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, "Documents", "Assetto Corsa", "setups"),
			filepath.Join(home, ".steam", "steam", "steamapps", "common", "assettocorsa", "setups"),
		)
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return DetectResult{Folder: c, Found: true}
		}
	}

	// Return a sensible default even if not on disk yet
	fallback := candidates[0]
	return DetectResult{Folder: fallback, Found: false}
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

// parseLDName handles: ks_vallelunga_&_tatuusfa1_&_driver_&_stint_22.ld
func parseLDName(name string) (track, car string, ok bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	// strip the extension regardless of case
	for _, ext := range []string{".ld", ".ldx"} {
		base = strings.TrimSuffix(strings.ToLower(base), ext)
	}
	parts := strings.Split(base, "_&_")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
	}
	return "", "", false
}

// parseGhostName handles: GHOST_CAR_E. Cavalli_tatuusfa1.ghost
func parseGhostName(name string) (car string, ok bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	lower := strings.ToLower(base)
	if !strings.HasPrefix(lower, "ghost_car_") {
		return "", false
	}
	// last underscore-delimited segment after the driver name
	idx := strings.LastIndex(base, "_")
	if idx < 0 {
		return "", false
	}
	car = strings.TrimSpace(base[idx+1:])
	if car == "" {
		return "", false
	}
	return strings.ToLower(car), true
}

// parseINICar reads the [CAR] MODEL= value from an ini file.
func parseINICar(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	inCar := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(line, "[CAR]") {
			inCar = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inCar = false
		}
		if inCar && strings.HasPrefix(strings.ToUpper(line), "MODEL=") {
			return strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Core import logic
// ---------------------------------------------------------------------------

func processZip(zipBytes []byte, zipName, setupsFolder string, overwrite bool) ImportResult {
	result := ImportResult{ZipName: zipName}

	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		result.Error = "Failed to read zip: " + err.Error()
		return result
	}

	var track, car string
	var iniFiles []*zip.File
	var carFromINI string

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(f.Name)
		lower := strings.ToLower(name)

		switch {
		case strings.HasSuffix(lower, ".ld"):
			if track == "" {
				t, c, ok := parseLDName(name)
				if ok {
					track, car = t, c
				}
			}

		case strings.HasSuffix(lower, ".ghost"):
			if car == "" {
				if c, ok := parseGhostName(name); ok {
					car = c
				}
			}

		case strings.HasSuffix(lower, ".ini"):
			iniFiles = append(iniFiles, f)
			// Read first ini for [CAR] MODEL= fallback
			if carFromINI == "" {
				rc, err := f.Open()
				if err == nil {
					content, _ := io.ReadAll(rc)
					rc.Close()
					carFromINI = parseINICar(content)
				}
			}
		}
	}

	// Fallback chain: .ld > .ghost > ini [CAR] MODEL
	if car == "" {
		car = strings.ToLower(carFromINI)
	}

	if car == "" || track == "" {
		result.Error = fmt.Sprintf(
			"Could not determine car/track — need a .ld file named like track_&_car_&_....ld (got car=%q track=%q)",
			car, track,
		)
		return result
	}

	result.Car = car
	result.Track = track

	if len(iniFiles) == 0 {
		result.Error = "No .ini setup files found in zip"
		return result
	}

	// Create destination directory
	destDir := filepath.Join(setupsFolder, car, track)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		result.Error = "Failed to create directory " + destDir + ": " + err.Error()
		return result
	}
	result.Destination = destDir

	// Copy ini files
	for _, f := range iniFiles {
		name := filepath.Base(f.Name)
		destPath := filepath.Join(destDir, name)

		if !overwrite {
			if _, err := os.Stat(destPath); err == nil {
				result.Skipped = append(result.Skipped, name)
				continue
			}
		}

		rc, err := f.Open()
		if err != nil {
			result.Error = "Failed to open " + name + ": " + err.Error()
			return result
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			result.Error = "Failed to read " + name + ": " + err.Error()
			return result
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			result.Error = "Failed to write " + name + ": " + err.Error()
			return result
		}
		result.Files = append(result.Files, name)
	}

	result.Success = true
	return result
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func handleDetectFolder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(detectSetupsFolder())
}

func handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(512 << 20); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	setupsFolder := strings.TrimSpace(r.FormValue("setupsFolder"))
	if setupsFolder == "" {
		setupsFolder = detectSetupsFolder().Folder
	}

	overwrite := r.FormValue("overwrite") == "true"

	var results []ImportResult

	fileHeaders := r.MultipartForm.File["zips"]
	if len(fileHeaders) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	for _, fh := range fileHeaders {
		f, err := fh.Open()
		if err != nil {
			results = append(results, ImportResult{ZipName: fh.Filename, Error: "Could not open upload: " + err.Error()})
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			results = append(results, ImportResult{ZipName: fh.Filename, Error: "Could not read upload: " + err.Error()})
			continue
		}
		results = append(results, processZip(data, fh.Filename, setupsFolder, overwrite))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// ---------------------------------------------------------------------------
// Browser launcher
// ---------------------------------------------------------------------------

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ---------------------------------------------------------------------------
// Embedded frontend
// ---------------------------------------------------------------------------

const frontendHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>DriveKit — Setup Importer</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Rajdhani:wght@500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

  :root {
    --bg:       #0e0e0e;
    --surface:  #161616;
    --border:   #252525;
    --border2:  #333;
    --accent:   #d4ff47;
    --accent2:  #a8cc2a;
    --text:     #e8e8e8;
    --muted:    #666;
    --success:  #4dff91;
    --error:    #ff4d6a;
    --warn:     #ffbb47;
    --font-h:   'Rajdhani', sans-serif;
    --font-m:   'JetBrains Mono', monospace;
  }

  html, body {
    height: 100%;
    background: var(--bg);
    color: var(--text);
    font-family: var(--font-m);
    font-size: 13px;
    line-height: 1.5;
  }

  /* ── Layout ── */
  .shell {
    min-height: 100vh;
    display: grid;
    grid-template-rows: auto 1fr;
  }

  /* ── Header ── */
  header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 18px 32px;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
  }
  .logo {
    font-family: var(--font-h);
    font-weight: 700;
    font-size: 22px;
    letter-spacing: 0.12em;
    color: var(--accent);
    text-transform: uppercase;
  }
  .logo span {
    color: var(--text);
    font-weight: 500;
  }
  .logo-sub {
    color: var(--muted);
    font-size: 14px;
    font-weight: 500;
    letter-spacing: 0.08em;
  }
  .badge {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.15em;
    text-transform: uppercase;
    color: var(--muted);
    padding: 3px 8px;
    border: 1px solid var(--border2);
    border-radius: 0;
  }

  /* ── Main content ── */
  main {
    padding: 32px;
    display: flex;
    flex-direction: column;
    gap: 24px;
    max-width: 860px;
    width: 100%;
    margin: 0 auto;
  }

  /* ── Folder row ── */
  .folder-row {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .section-label {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.18em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .folder-input-wrap {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .folder-input {
    flex: 1;
    background: var(--surface);
    border: 1px solid var(--border2);
    border-radius: 0;
    color: var(--text);
    font-family: var(--font-m);
    font-size: 12px;
    padding: 9px 14px;
    outline: none;
    transition: border-color 0.15s;
  }
  .folder-input:focus { border-color: var(--accent); }
  .folder-status {
    font-size: 11px;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .dot {
    width: 7px; height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .dot.found   { background: var(--success); box-shadow: 0 0 6px var(--success); }
  .dot.missing { background: var(--warn); box-shadow: 0 0 6px var(--warn); }

  /* ── Overwrite toggle ── */
  .options-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .toggle-label {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    font-size: 12px;
    color: var(--muted);
    user-select: none;
  }
  .toggle-label input[type=checkbox] {
    appearance: none;
    width: 32px; height: 18px;
    border: 1px solid var(--border2);
    border-radius: 0;
    background: var(--surface);
    cursor: pointer;
    position: relative;
    transition: background 0.2s, border-color 0.2s;
    flex-shrink: 0;
  }
  .toggle-label input[type=checkbox]::after {
    content: '';
    position: absolute;
    top: 2px; left: 2px;
    width: 12px; height: 12px;
    background: var(--muted);
    transition: transform 0.2s, background 0.2s;
  }
  .toggle-label input[type=checkbox]:checked {
    background: color-mix(in srgb, var(--accent) 15%, transparent);
    border-color: var(--accent);
  }
  .toggle-label input[type=checkbox]:checked::after {
    transform: translateX(14px);
    background: var(--accent);
  }

  /* ── Drop zone ── */
  .dropzone {
    border: 1.5px dashed var(--border2);
    border-radius: 0;
    padding: 48px 32px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 14px;
    cursor: pointer;
    transition: border-color 0.15s, background 0.15s;
    position: relative;
    text-align: center;
  }
  .dropzone:hover, .dropzone.drag-over {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }
  .dropzone input[type=file] {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
    width: 100%;
    height: 100%;
  }
  .dz-icon {
    width: 48px; height: 48px;
    opacity: 0.4;
  }
  .dz-main {
    font-family: var(--font-h);
    font-size: 18px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--text);
    opacity: 0.7;
  }
  .dz-sub {
    font-size: 11px;
    color: var(--muted);
    letter-spacing: 0.05em;
  }
  .dz-accent {
    color: var(--accent);
    font-weight: 600;
  }

  /* ── Processing indicator ── */
  .processing {
    display: none;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 0;
    font-size: 12px;
    color: var(--muted);
  }
  .processing.show { display: flex; }
  .spinner {
    width: 16px; height: 16px;
    border: 2px solid var(--border2);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Results ── */
  .results { display: flex; flex-direction: column; gap: 10px; }

  .result-card {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 0;
    overflow: hidden;
    animation: slideIn 0.2s ease;
  }
  @keyframes slideIn {
    from { opacity: 0; transform: translateY(6px); }
    to   { opacity: 1; transform: translateY(0); }
  }
  .result-card.ok   { border-left: 3px solid var(--success); }
  .result-card.fail { border-left: 3px solid var(--error); }
  .result-card.warn { border-left: 3px solid var(--warn); }

  .card-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
  }
  .card-icon { font-size: 15px; flex-shrink: 0; }
  .card-zip {
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pill {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    padding: 2px 8px;
    border-radius: 0;
    flex-shrink: 0;
  }
  .pill.ok   { background: color-mix(in srgb, var(--success) 15%, transparent); color: var(--success); }
  .pill.fail { background: color-mix(in srgb, var(--error)   15%, transparent); color: var(--error); }
  .pill.warn { background: color-mix(in srgb, var(--warn)    15%, transparent); color: var(--warn); }

  .card-body {
    padding: 0 16px 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .meta-grid {
    display: grid;
    grid-template-columns: 60px 1fr;
    gap: 4px 12px;
    font-size: 11px;
  }
  .meta-key { color: var(--muted); }
  .meta-val { color: var(--accent); font-weight: 500; }
  .meta-val.path { color: var(--text); font-size: 10px; word-break: break-all; }
  .meta-val.err  { color: var(--error); }

  .files-list {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 2px;
  }
  .file-chip {
    font-size: 10px;
    padding: 2px 8px;
    border-radius: 0;
    border: 1px solid var(--border2);
    color: var(--text);
    opacity: 0.75;
  }
  .file-chip.skipped {
    opacity: 0.4;
    text-decoration: line-through;
  }

  /* ── Button ── */
  .btn {
    font-family: var(--font-m);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    padding: 8px 18px;
    border-radius: 0;
    border: 1px solid transparent;
    cursor: pointer;
    transition: all 0.15s;
  }
  .btn-primary {
    background: var(--accent);
    color: #0e0e0e;
    border-color: var(--accent);
  }
  .btn-primary:hover { background: var(--accent2); border-color: var(--accent2); }
  .btn-ghost {
    background: transparent;
    color: var(--muted);
    border-color: var(--border2);
  }
  .btn-ghost:hover { border-color: var(--text); color: var(--text); }
  .btn-danger {
    background: transparent;
    color: var(--error);
    border-color: var(--error);
    opacity: 0.7;
  }
  .btn-danger:hover { opacity: 1; }

  /* ── Quit modal ── */
  .modal-overlay {
    display: none;
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.7);
    z-index: 100;
    align-items: center;
    justify-content: center;
  }
  .modal-overlay.open { display: flex; }
  .modal {
    background: var(--surface);
    border: 1px solid var(--border2);
    border-radius: 0;
    padding: 28px 32px;
    max-width: 340px;
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .modal-title {
    font-family: var(--font-h);
    font-size: 18px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--error);
  }
  .modal-body { color: var(--muted); font-size: 13px; line-height: 1.6; }
  .modal-actions { display: flex; gap: 10px; justify-content: flex-end; }

  /* ── Divider ── */
  hr { border: none; border-top: 1px solid var(--border); }
</style>
</head>
<body>
<div class="shell">
  <header>
    <div class="logo">Drive<span>Kit</span> <span class="logo-sub">Setup Importer</span></div>
    <button class="btn btn-danger" style="margin-left:auto" onclick="openQuitModal()">Quit</button>
  </header>

  <!-- Quit confirmation modal -->
  <div id="quitModal" class="modal-overlay">
    <div class="modal">
      <div class="modal-title">Quit Setup Importer?</div>
      <div class="modal-body">The app will stop running and close. You can relaunch it any time.</div>
      <div class="modal-actions">
        <button class="btn btn-ghost" onclick="closeQuitModal()">Cancel</button>
        <button class="btn btn-danger" onclick="confirmQuit()">Quit</button>
      </div>
    </div>
  </div>

  <main>
    <!-- Setups folder -->
    <div class="folder-row">
      <div class="section-label">Assetto Corsa Setups Folder</div>
      <div class="folder-input-wrap">
        <input id="folderInput" class="folder-input" type="text" placeholder="Detecting…" spellcheck="false" />
        <button class="btn btn-ghost" onclick="detectFolder()">Detect</button>
      </div>
      <div class="folder-status">
        <div id="folderDot" class="dot missing"></div>
        <span id="folderMsg" style="color:var(--muted)">Detecting setups folder…</span>
      </div>
    </div>

    <!-- Options -->
    <div class="options-row">
      <label class="toggle-label">
        <input type="checkbox" id="overwriteToggle" />
        Overwrite existing setups
      </label>
    </div>

    <hr />

    <!-- Drop zone -->
    <div class="dropzone" id="dropzone">
      <input type="file" id="fileInput" multiple accept=".zip" />
      <svg class="dz-icon" viewBox="0 0 48 48" fill="none" xmlns="http://www.w3.org/2000/svg">
        <rect x="6" y="10" width="36" height="28" rx="3" stroke="currentColor" stroke-width="2.5"/>
        <path d="M16 10V6h16v4" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
        <path d="M24 20v12M18 26l6-6 6 6" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
      </svg>
      <div class="dz-main">Drop setup zips here</div>
      <div class="dz-sub">or <span class="dz-accent">click to browse</span> — accepts multiple .zip files</div>
    </div>

    <!-- Processing -->
    <div class="processing" id="processing">
      <div class="spinner"></div>
      <span id="processingMsg">Importing setups…</span>
    </div>

    <!-- Results -->
    <div class="results" id="results"></div>
  </main>
</div>

<script>
const folderInput    = document.getElementById('folderInput');
const folderDot      = document.getElementById('folderDot');
const folderMsg      = document.getElementById('folderMsg');
const fileInput      = document.getElementById('fileInput');
const dropzone       = document.getElementById('dropzone');
const processing     = document.getElementById('processing');
const processingMsg  = document.getElementById('processingMsg');
const resultsEl      = document.getElementById('results');
const overwriteToggle= document.getElementById('overwriteToggle');

// ── Quit ──────────────────────────────────────────────────────────────────

function openQuitModal()  { document.getElementById('quitModal').classList.add('open'); }
function closeQuitModal() { document.getElementById('quitModal').classList.remove('open'); }
async function confirmQuit() {
  try { await fetch('/quit', { method: 'POST' }); } catch(_) {}
  window.close();
}

// ── Folder detection ──────────────────────────────────────────────────────

async function detectFolder() {
  folderMsg.textContent = 'Detecting…';
  folderDot.className = 'dot missing';
  try {
    const res = await fetch('/detect-folder');
    const data = await res.json();
    folderInput.value = data.folder;
    if (data.found) {
      folderDot.className = 'dot found';
      folderMsg.style.color = 'var(--success)';
      folderMsg.textContent = 'Folder found';
    } else {
      folderDot.className = 'dot missing';
      folderMsg.style.color = 'var(--warn)';
      folderMsg.textContent = 'Folder not found — will be created on first import';
    }
  } catch(e) {
    folderMsg.textContent = 'Detection failed';
  }
}

detectFolder();

// ── Drag & drop ───────────────────────────────────────────────────────────

['dragenter','dragover'].forEach(evt =>
  dropzone.addEventListener(evt, e => { e.preventDefault(); dropzone.classList.add('drag-over'); })
);
['dragleave','dragend','drop'].forEach(evt =>
  dropzone.addEventListener(evt, e => { e.preventDefault(); dropzone.classList.remove('drag-over'); })
);
dropzone.addEventListener('drop', e => {
  const files = [...e.dataTransfer.files].filter(f => f.name.endsWith('.zip'));
  if (files.length) importFiles(files);
});
fileInput.addEventListener('change', () => {
  const files = [...fileInput.files];
  if (files.length) importFiles(files);
  fileInput.value = '';
});

// ── Import ────────────────────────────────────────────────────────────────

async function importFiles(files) {
  processingMsg.textContent = files.length === 1
    ? 'Importing ' + files[0].name + '…'
    : 'Importing ' + files.length + ' zip files…';
  processing.classList.add('show');

  const form = new FormData();
  form.append('setupsFolder', folderInput.value.trim());
  form.append('overwrite', overwriteToggle.checked ? 'true' : 'false');
  files.forEach(f => form.append('zips', f));

  try {
    const res = await fetch('/import', { method: 'POST', body: form });
    const results = await res.json();
    results.forEach(r => renderResult(r));
  } catch(e) {
    renderError('Network error: ' + e.message);
  } finally {
    processing.classList.remove('show');
  }
}

// ── Render helpers ────────────────────────────────────────────────────────

function renderResult(r) {
  const card = document.createElement('div');

  if (!r.success && r.error) {
    card.className = 'result-card fail';
    card.innerHTML = '<div class="card-head">'
      + '<span class="card-icon">✗</span>'
      + '<span class="card-zip">' + esc(r.zipName) + '</span>'
      + '<span class="pill fail">Failed</span>'
      + '</div>'
      + '<div class="card-body">'
      + '<div class="meta-grid">'
      + '<span class="meta-key">Error</span>'
      + '<span class="meta-val err">' + esc(r.error) + '</span>'
      + '</div></div>';

  } else {
    const hasSkipped = r.skipped && r.skipped.length > 0;
    const hasFiles   = r.files   && r.files.length   > 0;
    const status     = hasSkipped && !hasFiles ? 'warn' : 'ok';
    const statusText = hasSkipped && !hasFiles ? 'Skipped'
                     : hasSkipped ? 'Partial' : 'Imported';
    const icon       = status === 'ok' ? '✓' : '⚠';

    let filesHtml = '';
    if (hasFiles) {
      filesHtml += '<div class="files-list">'
        + r.files.map(f => '<span class="file-chip">' + esc(f) + '</span>').join('')
        + '</div>';
    }
    if (hasSkipped) {
      filesHtml += '<div class="files-list">'
        + r.skipped.map(f => '<span class="file-chip skipped">' + esc(f) + '</span>').join('')
        + '</div>';
    }

    card.className = 'result-card ' + status;
    card.innerHTML = '<div class="card-head">'
      + '<span class="card-icon">' + icon + '</span>'
      + '<span class="card-zip">' + esc(r.zipName) + '</span>'
      + '<span class="pill ' + status + '">' + statusText + '</span>'
      + '</div>'
      + '<div class="card-body">'
      + '<div class="meta-grid">'
      + '<span class="meta-key">Car</span><span class="meta-val">'   + esc(r.car)   + '</span>'
      + '<span class="meta-key">Track</span><span class="meta-val">' + esc(r.track) + '</span>'
      + '<span class="meta-key">Path</span><span class="meta-val path">' + esc(r.destination) + '</span>'
      + '</div>'
      + filesHtml
      + '</div>';
  }

  resultsEl.prepend(card);
}

function renderError(msg) {
  const card = document.createElement('div');
  card.className = 'result-card fail';
  card.innerHTML = '<div class="card-head">'
    + '<span class="card-icon">✗</span>'
    + '<span class="card-zip">' + esc(msg) + '</span>'
    + '<span class="pill fail">Error</span>'
    + '</div>';
  resultsEl.prepend(card);
}

function esc(s) {
  return String(s ?? '')
    .replace(/&/g,'&amp;')
    .replace(/</g,'&lt;')
    .replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;');
}
</script>
</body>
</html>`

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, frontendHTML)
	})
	mux.HandleFunc("/detect-folder", handleDetectFolder)
	mux.HandleFunc("/import", handleImport)
	mux.HandleFunc("/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		go quitApp()
	})

	l, err := net.Listen("tcp", "127.0.0.1:7432")
	if err != nil {
		log.Fatalf("Could not bind port 7432: %v\n(Is another instance already running?)", err)
	}

	go func() {
		url := "http://localhost:7432"
		fmt.Println("┌─────────────────────────────────────────┐")
		fmt.Println("│   DriveKit Setup Importer               │")
		fmt.Printf("│   %s                      │\n", url)
		fmt.Println("└─────────────────────────────────────────┘")
		go openBrowser(url)
		log.Fatal(http.Serve(l, mux))
	}()

	startTray()
}
