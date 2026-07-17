package main

// AppVersion is the app's own version number, shown in the nav bar and the
// systray tooltip. There's no automated bump process -- edit the constant
// below by hand and rebuild whenever you want to release a new version.
const AppVersion = "1.0.1"

// CurrentSchemaVersion is the database schema version this build of the app
// expects. It's stored in the database itself (see ensureSchemaVersion in
// db.go) so an older/newer app build can tell whether the database it just
// opened matches, is older (and needs upgrading), or is newer (and should be
// refused rather than silently corrupted).
//
// Bump this by hand whenever you make a change to db.go that existing
// databases need to be migrated for, and add the matching upgrade step in
// ensureSchemaVersion's switch.
const CurrentSchemaVersion = 1
