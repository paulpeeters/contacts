package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// appSettings is persisted as JSON in dataDir (appsettings.json) and holds
// app-level settings that live outside the database -- currently which
// database file to open (has to be known *before* any database is open)
// and which port to listen on (has to be known before the HTTP server, or
// even the "already running?" check, starts). This is deliberately
// separate from the app_settings table in db.go (home_country etc.), which
// lives inside a specific database and travels with it.
type appSettings struct {
	LastDBPath string `json:"last_db_path"`

	// Port overrides the default listen port (8080, see defaultPort in
	// main.go) -- e.g. when another program on that PC already uses 8080.
	// 0/absent means "use the default". Only takes effect on the next
	// restart; there's no in-app UI for this yet, edit appsettings.json by
	// hand and restart Contacts.
	Port int `json:"port,omitempty"`
}

// appSettingsPath returns the full path to appsettings.json inside dir
// (dataDir -- exeDir/data -- for both the standalone exe and Docker).
func appSettingsPath(dir string) string {
	return filepath.Join(dir, "appsettings.json")
}

// loadAppSettings reads appsettings.json, returning a zero-value appSettings
// (LastDBPath == "") if the file doesn't exist yet or can't be parsed --
// callers should fall back to a sensible default in that case rather than
// treating it as a fatal error.
func loadAppSettings(dir string) appSettings {
	data, err := os.ReadFile(appSettingsPath(dir))
	if err != nil {
		return appSettings{}
	}
	var s appSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return appSettings{}
	}
	return s
}

// saveAppSettings writes appSettings back to appsettings.json as
// human-readable JSON.
func saveAppSettings(dir string, s appSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(appSettingsPath(dir), data, 0644)
}
