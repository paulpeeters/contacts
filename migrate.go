package main

import (
	"log"
	"os"
	"path/filepath"
)

// migrateLegacyDataFiles moves a pre-existing contacts.db and/or
// appsettings.json from the old default location (directly next to the
// exe) into dataDir (exeDir/data) -- the new default as of the ./data
// support added for Docker (a mounted volume needs its own dedicated
// folder), shared by both the standalone exe and the Docker image so their
// behavior stays consistent.
//
// Only ever touches files that were sitting at the OLD DEFAULT location --
// a database opened from a custom/explicit path (e.g. a UNC path chosen via
// the database-chooser modal) is never moved here, since that's a
// deliberate choice the user already made and has nothing to do with this
// default-location change.
func migrateLegacyDataFiles(exeDir, dataDir string) {
	legacySettingsPath := filepath.Join(exeDir, "appsettings.json")
	newSettingsPath := filepath.Join(dataDir, "appsettings.json")
	settingsMigrated := migrateLegacyFile(legacySettingsPath, newSettingsPath)

	legacyDBPath := filepath.Join(exeDir, "contacts.db")
	newDBPath := filepath.Join(dataDir, "contacts.db")
	migrateLegacyFile(legacyDBPath, newDBPath)

	// If appsettings.json just got migrated and its last_db_path pointed at
	// the old default contacts.db location (or the bare relative filename,
	// which is what it'd be if someone hand-typed it), repoint it at the
	// new default -- otherwise the app would go looking for a database
	// that's no longer there. A custom path the user explicitly chose
	// (e.g. a network share) is left exactly as-is.
	if settingsMigrated {
		s := loadAppSettings(dataDir)
		if s.LastDBPath == legacyDBPath || s.LastDBPath == "contacts.db" {
			s.LastDBPath = newDBPath
			if err := saveAppSettings(dataDir, s); err != nil {
				log.Printf("kon appsettings.json niet bijwerken na migratie: %v", err)
			}
		}
	}
}

// migrateLegacyFile moves src to dst if dst doesn't already exist and src
// does (no-op, returning false, in every other case -- including when dst
// already exists, which means either a previous run already migrated it or
// the new location was set up independently; either way src is left alone
// rather than overwriting something). Returns whether a move happened.
func migrateLegacyFile(src, dst string) bool {
	if _, err := os.Stat(dst); err == nil {
		return false
	}
	if _, err := os.Stat(src); err != nil {
		return false
	}
	if err := os.Rename(src, dst); err != nil {
		log.Printf("kon %s niet verplaatsen naar %s: %v", src, dst, err)
		return false
	}
	log.Printf("bestaand bestand gemigreerd naar de nieuwe datamap: %s -> %s", src, dst)
	return true
}
