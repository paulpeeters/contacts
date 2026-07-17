package main

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// handleChooseDBSubmit handles both forms in the database-chooser modal (see
// templates/home.html): "Wisselen" (mode=switch, open an existing/expected
// database file at the given path) and "Nieuwe database aanmaken" (mode=create,
// deliberately refuses if a file already exists there, so it can't be used
// to accidentally re-open -- and think you've started fresh with -- an
// existing database). Either way, it only swaps out the live global db
// (closing the old connection) once the new one has opened successfully --
// on failure the existing database keeps running untouched, and the user is
// sent back to /home with an error so the chooser modal reopens showing
// what went wrong.
func handleChooseDBSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(r.FormValue("db_path"))
	mode := r.FormValue("mode")
	if path == "" {
		redirectDBError(w, r, "Geef een pad naar een databasebestand op.", path)
		return
	}

	if mode == "create" {
		if _, statErr := os.Stat(path); statErr == nil {
			redirectDBError(w, r, "Er bestaat al een bestand op dit pad. Gebruik \"Wisselen\" om een bestaande database te openen, of kies een ander (nog niet bestaand) pad voor de nieuwe database.", path)
			return
		}
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				redirectDBError(w, r, "Kon de map voor het nieuwe databasebestand niet aanmaken: "+err.Error(), path)
				return
			}
		}
	}

	newDB, err := openDB(path)
	if err != nil {
		log.Printf("kon database %q niet openen/aanmaken: %v", path, err)
		redirectDBError(w, r, err.Error(), path)
		return
	}

	old := db
	db = newDB
	currentDBPath = path
	if old != nil {
		old.Close()
	}

	if err := saveAppSettings(exeDir, appSettings{LastDBPath: path, Port: currentPort}); err != nil {
		// Non-fatal: the database switch itself already succeeded, only the
		// "remember this for next time" step failed.
		log.Printf("kon appsettings.json niet bijwerken: %v", err)
	}

	if mode == "create" {
		log.Printf("nieuwe database aangemaakt: %s", path)
		http.Redirect(w, r, "/home?db_switched=1&db_created=1", http.StatusFound)
		return
	}
	log.Printf("gewisseld naar database: %s", path)
	http.Redirect(w, r, "/home?db_switched=1", http.StatusFound)
}

func redirectDBError(w http.ResponseWriter, r *http.Request, message, attempt string) {
	v := url.Values{}
	v.Set("db_error", message)
	v.Set("db_attempt", attempt)
	http.Redirect(w, r, "/home?"+v.Encode(), http.StatusFound)
}
