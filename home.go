package main

import "net/http"

// handleHome renders the app's landing page: a short explanation of what
// each menu item does, so a new user knows where to go before picking an
// option from the nav menu above. It also owns the database-chooser modal:
// the very first time /home loads in this process run, ShowDBChooser is set
// so the modal pops up automatically (pre-filled with the current path) --
// see dbChooserShown below and templates/home.html. After that, the user can
// still reopen it anytime via the "Database wisselen" button on the page,
// which needs no server round-trip since the modal's markup is always in
// the DOM.
func handleHome(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data := homeData{
		AppVersion: AppVersion,
		DBPath:     currentDBPath,
	}

	if !dbChooserShown {
		dbChooserShown = true
		data.ShowDBChooser = true
	}

	if q.Get("db_switched") == "1" {
		data.ShowDBSwitchedBanner = true
		data.DBCreated = q.Get("db_created") == "1"
	}
	if dbErr := q.Get("db_error"); dbErr != "" {
		data.DBError = dbErr
		data.DBAttempt = q.Get("db_attempt")
		data.ShowDBChooser = true
	}

	if q.Get("backup_done") == "1" {
		data.ShowBackupBanner = true
		data.BackupPath = q.Get("backup_path")
	}
	if backupErr := q.Get("backup_error"); backupErr != "" {
		data.ShowBackupBanner = true
		data.BackupError = backupErr
	}

	render(w, "home", data)
}
