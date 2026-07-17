package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// handleBackupDB creates a point-in-time copy of the currently open database
// next to the original file, named after it with a timestamp inserted
// before the extension -- e.g. contacts.db -> contacts-20260717-153012.db
// (see backupPathFor). Uses SQLite's own "VACUUM INTO" rather than copying
// the raw file bytes: VACUUM INTO takes a transactionally-consistent
// snapshot through SQLite itself, so it can't produce a torn/corrupt copy
// if a write happens to be in flight, and it works regardless of whether
// the database is using a rollback journal or WAL.
func handleBackupDB(w http.ResponseWriter, r *http.Request) {
	backupPath := backupPathFor(currentDBPath, time.Now())

	if _, err := db.Exec(`VACUUM INTO ?`, backupPath); err != nil {
		log.Printf("back-up maken mislukt (%s): %v", backupPath, err)
		v := url.Values{}
		v.Set("backup_error", err.Error())
		http.Redirect(w, r, "/home?"+v.Encode(), http.StatusFound)
		return
	}

	log.Printf("Back-up gemaakt: %s", backupPath)
	v := url.Values{}
	v.Set("backup_done", "1")
	v.Set("backup_path", backupPath)
	http.Redirect(w, r, "/home?"+v.Encode(), http.StatusFound)
}

// backupPathFor builds the backup file's path from the original database
// path and a timestamp, inserting "-YYYYMMDD-HHMMSS" right before the
// original file's extension so the backup sorts next to (and is obviously
// related to) the original in a file listing, e.g.:
// C:\Contacts\contacts.db -> C:\Contacts\contacts-20260717-153012.db
func backupPathFor(original string, t time.Time) string {
	dir := filepath.Dir(original)
	base := filepath.Base(original)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", name, t.Format("20060102-150405"), ext))
}
