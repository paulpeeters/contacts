package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// logFileName is the current run's log file, kept in dataDir (same folder
// as contacts.db and appsettings.json) so it's easy to find manually, e.g.
// to send along when reporting a problem.
const logFileName = "contacts.log"

// maxLogAge is how long an archived (rotated) log file is kept around
// before setupFileLogging deletes it on a later startup.
const maxLogAge = 30 * 24 * time.Hour

// setupFileLogging rotates any contacts.log left over from a previous run
// into a dated archive file (contacts-YYYYMMDD-HHMMSS.log, named after that
// file's own last-written time), opens a fresh contacts.log for this run,
// and deletes archived logs older than maxLogAge. It's meant to be layered
// on top of the existing stdout/in-app-ringbuffer logging (see main.go),
// not replace it -- so a failure here (e.g. the folder isn't writable) is
// returned as a plain error for the caller to log and continue past,
// rather than being treated as fatal.
func setupFileLogging(dir string) (*os.File, error) {
	path := filepath.Join(dir, logFileName)
	rotatePreviousLog(path)
	cleanupOldLogs(dir)
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

// rotatePreviousLog moves an existing contacts.log out of the way under a
// name that records when it was last written, so each run starts with a
// clean contacts.log instead of appending onto (or truncating) whatever a
// previous run left behind. If the rename fails for some reason, this run's
// lines simply get appended onto the existing file instead -- nothing is
// lost either way.
func rotatePreviousLog(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return // no previous contacts.log to rotate
	}
	archived := filepath.Join(filepath.Dir(path), fmt.Sprintf("contacts-%s.log", info.ModTime().Format("20060102-150405")))
	if err := os.Rename(path, archived); err != nil {
		log.Printf("kon vorige %s niet archiveren naar %s (logregels van deze sessie worden aan het bestaande bestand toegevoegd): %v", logFileName, archived, err)
	}
}

// cleanupOldLogs deletes archived log files (contacts-*.log, i.e. anything
// rotatePreviousLog created on an earlier run -- not the live contacts.log
// itself) whose last-modified time is older than maxLogAge. Run once at
// startup so archives don't pile up indefinitely.
func cleanupOldLogs(dir string) {
	cutoff := time.Now().Add(-maxLogAge)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "contacts-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				log.Printf("kon oud logbestand %s niet verwijderen: %v", name, err)
			} else {
				log.Printf("oud logbestand verwijderd (ouder dan 1 maand): %s", name)
			}
		}
	}
}
