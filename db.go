package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// openDB opens (creating if needed) the SQLite database file and ensures
// the schema exists.
func openDB(path string) (*sql.DB, error) {
	// Deliberately just the plain path -- no "?_pragma=..." suffix (that was
	// the previous approach). Appending a query string makes the driver
	// treat the DSN as a URI, and SQLite's URI-filename syntax needs a
	// specific, easy-to-get-wrong escaping for UNC paths (\\server\share\db
	// needs to become file://///server/share/db, five slashes) -- a UNC
	// path given as a plain path instead goes straight through SQLite's
	// ordinary (non-URI) filename handling, which has supported UNC paths
	// natively for a long time. The two PRAGMAs the query string used to set
	// are instead run as plain SQL right below.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	// SQLite handles concurrent writers poorly. A single connection keeps
	// this simple and avoids "database is locked" errors -- it also means
	// the PRAGMAs below (connection-scoped in SQLite) reliably stick for
	// every later query, since there's only ever the one connection.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS households (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	label       TEXT NOT NULL DEFAULT '',
	address     TEXT NOT NULL DEFAULT '',
	zip         TEXT NOT NULL DEFAULT '',
	city        TEXT NOT NULL DEFAULT '',
	country     TEXT NOT NULL DEFAULT '',
	phone       TEXT NOT NULL DEFAULT '',
	email       TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	// household_id has no NOT NULL constraint at the SQL level: existing
	// databases go through migrateContactsToHouseholds below to backfill it
	// via ALTER TABLE, and SQLite can't add a NOT NULL column without a
	// default to an existing table with rows. The application always sets
	// it, in effect treating it as required.
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS contacts (
	id                INTEGER PRIMARY KEY AUTOINCREMENT,
	household_id      INTEGER REFERENCES households(id),
	first_name        TEXT NOT NULL,
	last_name         TEXT NOT NULL,
	gender            TEXT NOT NULL DEFAULT '',
	birthdate         TEXT NOT NULL DEFAULT '',
	mobile            TEXT NOT NULL DEFAULT '',
	email             TEXT NOT NULL DEFAULT '',
	tags              TEXT NOT NULL DEFAULT '',
	last_verified_on  TEXT NOT NULL DEFAULT '',
	created_at        TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	if err := migrateContactsToHouseholds(db); err != nil {
		return nil, err
	}

	if err := migrateContactPersonalEmail(db); err != nil {
		return nil, err
	}

	// Relations between contacts were removed: households fully cover the
	// "these people belong together" case, and the generic relation link
	// (e.g. "family", "colleague") was unused overhead on top of that. Unlike
	// label_templates below, the user explicitly asked for the old table to
	// actually be gone, not just dormant -- DROP IF EXISTS is safe to run on
	// every startup (a no-op once it's already gone).
	if _, err := db.Exec(`DROP TABLE IF EXISTS contact_relations`); err != nil {
		return nil, err
	}

	// label_templates is the old, single-table label design (paper/grid,
	// margins/gaps, padding and elements all in one row). It's superseded by
	// the three-way label_sheets/label_contents/label_filters split below,
	// but the CREATE TABLE stays here (a no-op on any database that no
	// longer needs it) purely so migrateLabelTemplatesSplit below has
	// something to read from on a database that still has old rows in it.
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS label_templates (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	name                TEXT NOT NULL UNIQUE,
	paper_width_mm      REAL NOT NULL DEFAULT 210,
	paper_height_mm     REAL NOT NULL DEFAULT 297,
	cols                INTEGER NOT NULL DEFAULT 1,
	label_rows          INTEGER NOT NULL DEFAULT 1,
	margin_left_mm      REAL NOT NULL DEFAULT 0,
	margin_right_mm     REAL NOT NULL DEFAULT 0,
	margin_top_mm       REAL NOT NULL DEFAULT 0,
	margin_bottom_mm    REAL NOT NULL DEFAULT 0,
	gap_h_mm            REAL NOT NULL DEFAULT 0,
	gap_v_mm            REAL NOT NULL DEFAULT 0,
	padding_top_mm      REAL NOT NULL DEFAULT 0,
	padding_right_mm    REAL NOT NULL DEFAULT 0,
	padding_bottom_mm   REAL NOT NULL DEFAULT 0,
	padding_left_mm     REAL NOT NULL DEFAULT 0,
	elements_json       TEXT NOT NULL DEFAULT '[]',
	created_at          TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS label_sheets (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	name                TEXT NOT NULL UNIQUE,
	paper_width_mm      REAL NOT NULL DEFAULT 210,
	paper_height_mm     REAL NOT NULL DEFAULT 297,
	cols                INTEGER NOT NULL DEFAULT 1,
	label_rows          INTEGER NOT NULL DEFAULT 1,
	margin_left_mm      REAL NOT NULL DEFAULT 0,
	margin_right_mm     REAL NOT NULL DEFAULT 0,
	margin_top_mm       REAL NOT NULL DEFAULT 0,
	margin_bottom_mm    REAL NOT NULL DEFAULT 0,
	gap_h_mm            REAL NOT NULL DEFAULT 0,
	gap_v_mm            REAL NOT NULL DEFAULT 0,
	created_at          TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS label_contents (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	name                TEXT NOT NULL UNIQUE,
	padding_top_mm      REAL NOT NULL DEFAULT 0,
	padding_right_mm    REAL NOT NULL DEFAULT 0,
	padding_bottom_mm   REAL NOT NULL DEFAULT 0,
	padding_left_mm     REAL NOT NULL DEFAULT 0,
	elements_json       TEXT NOT NULL DEFAULT '[]',
	created_at          TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	// sheet_id/content_id intentionally have no ON DELETE action: deleting a
	// sheet or content that's still referenced by a saved filter is blocked
	// in the application layer (see countFiltersUsingSheet/Content), the
	// same way household deletes are blocked while members still exist.
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS label_filters (
	id                  INTEGER PRIMARY KEY AUTOINCREMENT,
	name                TEXT NOT NULL UNIQUE,
	sheet_id            INTEGER NOT NULL REFERENCES label_sheets(id),
	content_id          INTEGER NOT NULL REFERENCES label_contents(id),
	print_mode          TEXT NOT NULL DEFAULT 'contact',
	search_term         TEXT NOT NULL DEFAULT '',
	tags                TEXT NOT NULL DEFAULT '',
	created_at          TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
		return nil, err
	}

	if err := migrateLabelTemplatesSplit(db); err != nil {
		return nil, err
	}

	// Single-row settings table (id is always 1). home_country drives the
	// {ForeignCountry} label placeholder: it renders blank when a
	// household's country matches this setting, since local mail
	// conventionally doesn't print the country at all.
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS app_settings (
	id            INTEGER PRIMARY KEY CHECK (id = 1),
	home_country  TEXT NOT NULL DEFAULT 'België'
)`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO app_settings (id, home_country) VALUES (1, 'België')`); err != nil {
		return nil, err
	}

	if err := ensureSchemaVersion(db); err != nil {
		return nil, err
	}

	return db, nil
}

// ensureSchemaVersion records/checks the database schema version against
// CurrentSchemaVersion (see version.go). This is a safety net layered on top
// of the ordinary CREATE TABLE IF NOT EXISTS / ALTER TABLE migrations above:
// those keep any database self-upgrading regardless of version, but give no
// way to tell "this database was last opened by a much newer app build and
// may contain columns/tables this build doesn't understand" from "this is
// just an old database that still needs upgrading".
//
//   - No schema_meta row yet: a brand new (or pre-versioning) database --
//     stamp it with CurrentSchemaVersion and continue.
//   - Stored version > CurrentSchemaVersion: this database was written by a
//     newer app build. Refuse to open it rather than risk silently
//     misinterpreting/corrupting data this build doesn't know about.
//   - Stored version < CurrentSchemaVersion: run any upgrade steps between
//     the stored version and CurrentSchemaVersion, then bump the stored
//     value. There are no upgrade steps yet (CurrentSchemaVersion is still
//     1) -- add a case to the switch below the next time it's bumped.
func ensureSchemaVersion(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS schema_meta (
	id      INTEGER PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL
)`); err != nil {
		return err
	}

	var stored int
	err := db.QueryRow(`SELECT version FROM schema_meta WHERE id = 1`).Scan(&stored)
	if err == sql.ErrNoRows {
		_, err = db.Exec(`INSERT INTO schema_meta (id, version) VALUES (1, ?)`, CurrentSchemaVersion)
		return err
	}
	if err != nil {
		return err
	}

	if stored > CurrentSchemaVersion {
		return fmt.Errorf(
			"deze database heeft schemaversie %d, maar deze versie van Contacts (app v%s) kent enkel versie %d of ouder. "+
				"Update de app naar een nieuwere versie om deze database te openen.",
			stored, AppVersion, CurrentSchemaVersion)
	}

	for stored < CurrentSchemaVersion {
		stored++
		switch stored {
		// case 2: run any migration needed to go from schema version 1 to 2 here.
		default:
			// No upgrade steps defined for this version yet -- just bump the
			// stored number and move on.
		}
	}

	_, err = db.Exec(`UPDATE schema_meta SET version = ? WHERE id = 1`, stored)
	return err
}

func getHomeCountry(db *sql.DB) (string, error) {
	var c string
	err := db.QueryRow(`SELECT home_country FROM app_settings WHERE id = 1`).Scan(&c)
	return c, err
}

func updateHomeCountry(db *sql.DB, country string) error {
	_, err := db.Exec(`UPDATE app_settings SET home_country = ? WHERE id = 1`, country)
	return err
}

// columnExists reports whether the given table has a column with the given
// name, used to detect whether a not-yet-migrated (pre-household) database
// is being opened.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, pk int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateContactsToHouseholds upgrades a database created before households
// existed. It never drops or rewrites existing data: it only adds the new
// household_id column and, for any contact that doesn't have one yet,
// creates a one-person household from that contact's old address/phone/
// email columns (which are left in place, just unused going forward).
// Safe to call on every startup -- it's a no-op once household_id exists.
func migrateContactsToHouseholds(db *sql.DB) error {
	hasCol, err := columnExists(db, "contacts", "household_id")
	if err != nil {
		return err
	}
	if hasCol {
		return nil
	}

	if _, err := db.Exec(`ALTER TABLE contacts ADD COLUMN household_id INTEGER REFERENCES households(id)`); err != nil {
		return err
	}

	rows, err := db.Query(`
		SELECT id, first_name, last_name, address, zip, city, country, phone, email
		FROM contacts WHERE household_id IS NULL`)
	if err != nil {
		return err
	}
	type oldContact struct {
		id                                                             int64
		firstName, lastName, address, zip, city, country, phone, email string
	}
	var toMigrate []oldContact
	for rows.Next() {
		var o oldContact
		if err := rows.Scan(&o.id, &o.firstName, &o.lastName, &o.address, &o.zip,
			&o.city, &o.country, &o.phone, &o.email); err != nil {
			rows.Close()
			return err
		}
		toMigrate = append(toMigrate, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, o := range toMigrate {
		label := strings.TrimSpace(o.firstName + " " + o.lastName)
		res, err := db.Exec(`
			INSERT INTO households (label, address, zip, city, country, phone, email)
			VALUES (?,?,?,?,?,?,?)`,
			label, o.address, o.zip, o.city, o.country, o.phone, o.email)
		if err != nil {
			return err
		}
		hid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := db.Exec(`UPDATE contacts SET household_id = ? WHERE id = ?`, hid, o.id); err != nil {
			return err
		}
	}
	return nil
}

// migrateContactPersonalEmail adds a personal-email column to contacts if
// it's missing. Databases that predate the household split already have an
// "email" column here -- back then it was the contact's only email, before
// household-level shared email existed. This migration leaves that column
// and its data untouched and simply lets the app use it again as the
// personal email, rather than trying to add a second column with the same
// name (which columnExists is what prevents here).
func migrateContactPersonalEmail(db *sql.DB) error {
	hasCol, err := columnExists(db, "contacts", "email")
	if err != nil {
		return err
	}
	if hasCol {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE contacts ADD COLUMN email TEXT NOT NULL DEFAULT ''`)
	return err
}

// (listContacts, previously used to populate the "related contact" picker
// on the now-removed relations feature, was removed as dead code alongside
// it -- getContact/listContactsWithHousehold/listContactsForExport cover
// every remaining need.)

// listContactsWithHousehold is like listContacts but also joins in the
// household's label and city, for the contact list page.
func listContactsWithHousehold(db *sql.DB) ([]ContactListRow, error) {
	rows, err := db.Query(`
		SELECT c.id, c.household_id, c.first_name, c.last_name, c.gender, c.birthdate,
		       c.mobile, c.email, c.tags, c.last_verified_on, c.created_at, c.updated_at,
		       h.label, h.city
		FROM contacts c
		JOIN households h ON h.id = c.household_id
		ORDER BY c.last_name, c.first_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ContactListRow
	for rows.Next() {
		var r ContactListRow
		if err := rows.Scan(&r.ID, &r.HouseholdID, &r.FirstName, &r.LastName, &r.Gender,
			&r.Birthdate, &r.Mobile, &r.Email, &r.Tags, &r.LastVerifiedOn, &r.CreatedAt, &r.UpdatedAt,
			&r.HouseholdLabel, &r.City); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// listDistinctContactTags returns every distinct, trimmed tag used by any
// contact (tags are stored comma-separated per contact), sorted
// alphabetically -- used to populate the checkbox tag picker on the contact
// form.
func listDistinctContactTags(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT tags FROM contacts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var tags string
		if err := rows.Scan(&tags); err != nil {
			return nil, err
		}
		for _, tag := range strings.Split(tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				seen[tag] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}

func getContact(db *sql.DB, id int64) (*Contact, error) {
	var c Contact
	row := db.QueryRow(`
		SELECT id, household_id, first_name, last_name, gender, birthdate, mobile,
		       email, tags, last_verified_on, created_at, updated_at
		FROM contacts WHERE id = ?`, id)
	if err := row.Scan(&c.ID, &c.HouseholdID, &c.FirstName, &c.LastName, &c.Gender,
		&c.Birthdate, &c.Mobile, &c.Email, &c.Tags, &c.LastVerifiedOn, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func createContact(db *sql.DB, c Contact) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO contacts
			(household_id, first_name, last_name, gender, birthdate, mobile, email, tags, last_verified_on)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		c.HouseholdID, c.FirstName, c.LastName, c.Gender, c.Birthdate, c.Mobile, c.Email, c.Tags, c.LastVerifiedOn)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateContact(db *sql.DB, c Contact) error {
	_, err := db.Exec(`
		UPDATE contacts SET
			household_id = ?, first_name = ?, last_name = ?, gender = ?, birthdate = ?,
			mobile = ?, email = ?, tags = ?, last_verified_on = ?, updated_at = datetime('now')
		WHERE id = ?`,
		c.HouseholdID, c.FirstName, c.LastName, c.Gender, c.Birthdate, c.Mobile, c.Email, c.Tags, c.LastVerifiedOn, c.ID)
	return err
}

func deleteContact(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM contacts WHERE id = ?`, id)
	return err
}

// listContactsForLabels returns every contact with its household's shared
// fields joined in flat, for label printing (selection screen + batch PDF).
func listContactsForLabels(db *sql.DB) ([]LabelContact, error) {
	rows, err := db.Query(`
		SELECT c.id, c.household_id, c.first_name, c.last_name, c.gender, c.birthdate,
		       c.mobile, c.email, c.tags, c.last_verified_on, c.created_at, c.updated_at,
		       h.label, h.address, h.zip, h.city, h.country, h.phone, h.email
		FROM contacts c
		JOIN households h ON h.id = c.household_id
		ORDER BY c.last_name, c.first_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LabelContact
	for rows.Next() {
		var lc LabelContact
		// lc.Contact.Email is the contact's personal email (c.email); lc.Email
		// (LabelContact's own field) is the household's shared email (h.email)
		// -- see the note on LabelContact in models.go.
		if err := rows.Scan(&lc.ID, &lc.HouseholdID, &lc.FirstName, &lc.LastName, &lc.Gender,
			&lc.Birthdate, &lc.Mobile, &lc.Contact.Email, &lc.Tags, &lc.LastVerifiedOn, &lc.CreatedAt, &lc.UpdatedAt,
			&lc.HouseholdLabel, &lc.Address, &lc.Zip, &lc.City, &lc.Country, &lc.Phone, &lc.Email); err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

// listContactsForExport is listContactsForLabels' twin for the Excel
// export/sync feature: same joined fields, but ordered by household so
// members of the same household land on adjacent rows (easier to eyeball
// that the duplicated household columns actually match).
func listContactsForExport(db *sql.DB) ([]LabelContact, error) {
	rows, err := db.Query(`
		SELECT c.id, c.household_id, c.first_name, c.last_name, c.gender, c.birthdate,
		       c.mobile, c.email, c.tags, c.last_verified_on, c.created_at, c.updated_at,
		       h.label, h.address, h.zip, h.city, h.country, h.phone, h.email
		FROM contacts c
		JOIN households h ON h.id = c.household_id
		ORDER BY h.label, c.first_name, c.last_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LabelContact
	for rows.Next() {
		var lc LabelContact
		if err := rows.Scan(&lc.ID, &lc.HouseholdID, &lc.FirstName, &lc.LastName, &lc.Gender,
			&lc.Birthdate, &lc.Mobile, &lc.Contact.Email, &lc.Tags, &lc.LastVerifiedOn, &lc.CreatedAt, &lc.UpdatedAt,
			&lc.HouseholdLabel, &lc.Address, &lc.Zip, &lc.City, &lc.Country, &lc.Phone, &lc.Email); err != nil {
			return nil, err
		}
		out = append(out, lc)
	}
	return out, rows.Err()
}

// getContactForLabels is the single-contact equivalent of listContactsForLabels.
func getContactForLabels(db *sql.DB, id int64) (*LabelContact, error) {
	var lc LabelContact
	row := db.QueryRow(`
		SELECT c.id, c.household_id, c.first_name, c.last_name, c.gender, c.birthdate,
		       c.mobile, c.email, c.tags, c.last_verified_on, c.created_at, c.updated_at,
		       h.label, h.address, h.zip, h.city, h.country, h.phone, h.email
		FROM contacts c
		JOIN households h ON h.id = c.household_id
		WHERE c.id = ?`, id)
	if err := row.Scan(&lc.ID, &lc.HouseholdID, &lc.FirstName, &lc.LastName, &lc.Gender,
		&lc.Birthdate, &lc.Mobile, &lc.Contact.Email, &lc.Tags, &lc.LastVerifiedOn, &lc.CreatedAt, &lc.UpdatedAt,
		&lc.HouseholdLabel, &lc.Address, &lc.Zip, &lc.City, &lc.Country, &lc.Phone, &lc.Email); err != nil {
		return nil, err
	}
	return &lc, nil
}

// ---- households -------------------------------------------------------

func listHouseholds(db *sql.DB) ([]Household, error) {
	rows, err := db.Query(`
		SELECT id, label, address, zip, city, country, phone, email, created_at, updated_at
		FROM households ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Household
	for rows.Next() {
		var h Household
		if err := rows.Scan(&h.ID, &h.Label, &h.Address, &h.Zip, &h.City, &h.Country,
			&h.Phone, &h.Email, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func getHousehold(db *sql.DB, id int64) (*Household, error) {
	var h Household
	row := db.QueryRow(`
		SELECT id, label, address, zip, city, country, phone, email, created_at, updated_at
		FROM households WHERE id = ?`, id)
	if err := row.Scan(&h.ID, &h.Label, &h.Address, &h.Zip, &h.City, &h.Country,
		&h.Phone, &h.Email, &h.CreatedAt, &h.UpdatedAt); err != nil {
		return nil, err
	}
	return &h, nil
}

func createHousehold(db *sql.DB, h Household) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO households (label, address, zip, city, country, phone, email)
		VALUES (?,?,?,?,?,?,?)`,
		h.Label, h.Address, h.Zip, h.City, h.Country, h.Phone, h.Email)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateHousehold(db *sql.DB, h Household) error {
	_, err := db.Exec(`
		UPDATE households SET
			label = ?, address = ?, zip = ?, city = ?, country = ?, phone = ?, email = ?,
			updated_at = datetime('now')
		WHERE id = ?`,
		h.Label, h.Address, h.Zip, h.City, h.Country, h.Phone, h.Email, h.ID)
	return err
}

func deleteHousehold(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM households WHERE id = ?`, id)
	return err
}

// listHouseholdsWithMembers returns every household together with its
// current members, for the households list page. A simple N+1 query loop
// is fine here: household counts stay small for this kind of contact list.
func listHouseholdsWithMembers(db *sql.DB) ([]householdListRow, error) {
	households, err := listHouseholds(db)
	if err != nil {
		return nil, err
	}
	out := make([]householdListRow, 0, len(households))
	for _, h := range households {
		members, err := listHouseholdMembers(db, h.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, householdListRow{Household: h, Members: members})
	}
	return out, nil
}

func countHouseholdMembers(db *sql.DB, id int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM contacts WHERE household_id = ?`, id).Scan(&n)
	return n, err
}

func listHouseholdMembers(db *sql.DB, id int64) ([]Contact, error) {
	rows, err := db.Query(`
		SELECT id, household_id, first_name, last_name, gender, birthdate, mobile,
		       email, tags, last_verified_on, created_at, updated_at
		FROM contacts WHERE household_id = ? ORDER BY first_name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.HouseholdID, &c.FirstName, &c.LastName, &c.Gender,
			&c.Birthdate, &c.Mobile, &c.Email, &c.Tags, &c.LastVerifiedOn, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}


// findContactByNameAndBirthdate looks up an existing contact by first name,
// last name and birthdate (case-insensitive on the names). Used by the Excel
// import to detect duplicates. Returns (nil, nil) if nothing matches.
func findContactByNameAndBirthdate(db *sql.DB, firstName, lastName, birthdate string) (*Contact, error) {
	if firstName == "" && lastName == "" {
		return nil, nil
	}
	var c Contact
	row := db.QueryRow(`
		SELECT id, household_id, first_name, last_name, gender, birthdate, mobile,
		       email, tags, last_verified_on, created_at, updated_at
		FROM contacts
		WHERE lower(first_name) = lower(?) AND lower(last_name) = lower(?) AND birthdate = ?
		LIMIT 1`, firstName, lastName, birthdate)
	err := row.Scan(&c.ID, &c.HouseholdID, &c.FirstName, &c.LastName, &c.Gender,
		&c.Birthdate, &c.Mobile, &c.Email, &c.Tags, &c.LastVerifiedOn, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// migrateLabelTemplatesSplit runs once: it splits every row of the old,
// single-table label_templates (paper/grid/margins/gaps + padding +
// elements all bundled together) into the new three-way model -- a
// LabelSheet, a LabelContent and a LabelFilter, all sharing the old
// template's name -- so existing saved templates keep working (now backed
// by three linked records instead of one) without any manual rebuilding.
// Guarded on label_sheets being empty, so this only ever actually copies
// data once; a fresh database has nothing in label_templates and just
// skips straight through.
func migrateLabelTemplatesSplit(db *sql.DB) error {
	var sheetCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM label_sheets`).Scan(&sheetCount); err != nil {
		return err
	}
	if sheetCount > 0 {
		return nil
	}

	rows, err := db.Query(`
		SELECT id, name, paper_width_mm, paper_height_mm, cols, label_rows,
		       margin_left_mm, margin_right_mm, margin_top_mm, margin_bottom_mm, gap_h_mm, gap_v_mm,
		       padding_top_mm, padding_right_mm, padding_bottom_mm, padding_left_mm, elements_json
		FROM label_templates`)
	if err != nil {
		return err
	}

	type oldTemplate struct {
		name                                             string
		paperW, paperH                                   float64
		cols, labelRows                                  int
		marginL, marginR, marginT, marginB, gapH, gapV   float64
		padT, padR, padB, padL                           float64
		elementsJSON                                     string
	}
	var templates []oldTemplate
	for rows.Next() {
		var id int64
		var t oldTemplate
		if serr := rows.Scan(&id, &t.name, &t.paperW, &t.paperH, &t.cols, &t.labelRows,
			&t.marginL, &t.marginR, &t.marginT, &t.marginB, &t.gapH, &t.gapV,
			&t.padT, &t.padR, &t.padB, &t.padL, &t.elementsJSON); serr != nil {
			rows.Close()
			return serr
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, t := range templates {
		res, err := db.Exec(`
			INSERT INTO label_sheets
				(name, paper_width_mm, paper_height_mm, cols, label_rows,
				 margin_left_mm, margin_right_mm, margin_top_mm, margin_bottom_mm, gap_h_mm, gap_v_mm)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			t.name, t.paperW, t.paperH, t.cols, t.labelRows,
			t.marginL, t.marginR, t.marginT, t.marginB, t.gapH, t.gapV)
		if err != nil {
			return err
		}
		sheetID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		res, err = db.Exec(`
			INSERT INTO label_contents
				(name, padding_top_mm, padding_right_mm, padding_bottom_mm, padding_left_mm, elements_json)
			VALUES (?,?,?,?,?,?)`,
			t.name, t.padT, t.padR, t.padB, t.padL, t.elementsJSON)
		if err != nil {
			return err
		}
		contentID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		if _, err := db.Exec(`
			INSERT INTO label_filters (name, sheet_id, content_id, print_mode, search_term, tags)
			VALUES (?,?,?,'contact','','')`,
			t.name, sheetID, contentID); err != nil {
			return err
		}
	}

	return nil
}

const labelSheetColumns = `
	id, name, paper_width_mm, paper_height_mm, cols, label_rows,
	margin_left_mm, margin_right_mm, margin_top_mm, margin_bottom_mm,
	gap_h_mm, gap_v_mm, created_at, updated_at`

func scanLabelSheet(scan func(...any) error) (LabelSheet, error) {
	var s LabelSheet
	err := scan(&s.ID, &s.Name, &s.PaperWidthMM, &s.PaperHeightMM, &s.Cols, &s.Rows,
		&s.MarginLeftMM, &s.MarginRightMM, &s.MarginTopMM, &s.MarginBottomMM,
		&s.GapHMM, &s.GapVMM, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func listLabelSheets(db *sql.DB) ([]LabelSheet, error) {
	rows, err := db.Query(`SELECT` + labelSheetColumns + ` FROM label_sheets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LabelSheet
	for rows.Next() {
		s, err := scanLabelSheet(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func getLabelSheet(db *sql.DB, id int64) (*LabelSheet, error) {
	row := db.QueryRow(`SELECT`+labelSheetColumns+` FROM label_sheets WHERE id = ?`, id)
	s, err := scanLabelSheet(row.Scan)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func createLabelSheet(db *sql.DB, s LabelSheet) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO label_sheets
			(name, paper_width_mm, paper_height_mm, cols, label_rows,
			 margin_left_mm, margin_right_mm, margin_top_mm, margin_bottom_mm, gap_h_mm, gap_v_mm)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		s.Name, s.PaperWidthMM, s.PaperHeightMM, s.Cols, s.Rows,
		s.MarginLeftMM, s.MarginRightMM, s.MarginTopMM, s.MarginBottomMM, s.GapHMM, s.GapVMM)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateLabelSheet(db *sql.DB, s LabelSheet) error {
	_, err := db.Exec(`
		UPDATE label_sheets SET
			name = ?, paper_width_mm = ?, paper_height_mm = ?, cols = ?, label_rows = ?,
			margin_left_mm = ?, margin_right_mm = ?, margin_top_mm = ?, margin_bottom_mm = ?,
			gap_h_mm = ?, gap_v_mm = ?, updated_at = datetime('now')
		WHERE id = ?`,
		s.Name, s.PaperWidthMM, s.PaperHeightMM, s.Cols, s.Rows,
		s.MarginLeftMM, s.MarginRightMM, s.MarginTopMM, s.MarginBottomMM, s.GapHMM, s.GapVMM, s.ID)
	return err
}

func deleteLabelSheet(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM label_sheets WHERE id = ?`, id)
	return err
}

// countFiltersUsingSheet is used to block deleting a sheet that a saved
// filter still points at (mirrors countHouseholdMembers/the household
// delete guard).
func countFiltersUsingSheet(db *sql.DB, sheetID int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM label_filters WHERE sheet_id = ?`, sheetID).Scan(&n)
	return n, err
}

const labelContentColumns = `
	id, name, padding_top_mm, padding_right_mm, padding_bottom_mm, padding_left_mm,
	elements_json, created_at, updated_at`

func scanLabelContent(scan func(...any) error) (LabelContent, error) {
	var c LabelContent
	var elementsJSON string
	err := scan(&c.ID, &c.Name, &c.PaddingTopMM, &c.PaddingRightMM, &c.PaddingBottomMM, &c.PaddingLeftMM,
		&elementsJSON, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if elementsJSON != "" {
		if jerr := json.Unmarshal([]byte(elementsJSON), &c.Elements); jerr != nil {
			// Don't fail the whole page over corrupt JSON; just show no elements.
			c.Elements = nil
		}
	}
	return c, nil
}

func listLabelContents(db *sql.DB) ([]LabelContent, error) {
	rows, err := db.Query(`SELECT` + labelContentColumns + ` FROM label_contents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LabelContent
	for rows.Next() {
		c, err := scanLabelContent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func getLabelContent(db *sql.DB, id int64) (*LabelContent, error) {
	row := db.QueryRow(`SELECT`+labelContentColumns+` FROM label_contents WHERE id = ?`, id)
	c, err := scanLabelContent(row.Scan)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func createLabelContent(db *sql.DB, c LabelContent) (int64, error) {
	elementsJSON, err := json.Marshal(c.Elements)
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(`
		INSERT INTO label_contents
			(name, padding_top_mm, padding_right_mm, padding_bottom_mm, padding_left_mm, elements_json)
		VALUES (?,?,?,?,?,?)`,
		c.Name, c.PaddingTopMM, c.PaddingRightMM, c.PaddingBottomMM, c.PaddingLeftMM, string(elementsJSON))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateLabelContent(db *sql.DB, c LabelContent) error {
	elementsJSON, err := json.Marshal(c.Elements)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		UPDATE label_contents SET
			name = ?, padding_top_mm = ?, padding_right_mm = ?, padding_bottom_mm = ?, padding_left_mm = ?,
			elements_json = ?, updated_at = datetime('now')
		WHERE id = ?`,
		c.Name, c.PaddingTopMM, c.PaddingRightMM, c.PaddingBottomMM, c.PaddingLeftMM, string(elementsJSON), c.ID)
	return err
}

func deleteLabelContent(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM label_contents WHERE id = ?`, id)
	return err
}

// countFiltersUsingContent is used to block deleting a content that a saved
// filter still points at.
func countFiltersUsingContent(db *sql.DB, contentID int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM label_filters WHERE content_id = ?`, contentID).Scan(&n)
	return n, err
}

const labelFilterColumns = `
	id, name, sheet_id, content_id, print_mode, search_term, tags, created_at, updated_at`

func scanLabelFilter(scan func(...any) error) (LabelFilter, error) {
	var f LabelFilter
	err := scan(&f.ID, &f.Name, &f.SheetID, &f.ContentID, &f.PrintMode, &f.SearchTerm, &f.Tags, &f.CreatedAt, &f.UpdatedAt)
	return f, err
}

func listLabelFilters(db *sql.DB) ([]LabelFilter, error) {
	rows, err := db.Query(`SELECT` + labelFilterColumns + ` FROM label_filters ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LabelFilter
	for rows.Next() {
		f, err := scanLabelFilter(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func getLabelFilter(db *sql.DB, id int64) (*LabelFilter, error) {
	row := db.QueryRow(`SELECT`+labelFilterColumns+` FROM label_filters WHERE id = ?`, id)
	f, err := scanLabelFilter(row.Scan)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func createLabelFilter(db *sql.DB, f LabelFilter) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO label_filters (name, sheet_id, content_id, print_mode, search_term, tags)
		VALUES (?,?,?,?,?,?)`,
		f.Name, f.SheetID, f.ContentID, f.PrintMode, f.SearchTerm, f.Tags)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func updateLabelFilter(db *sql.DB, f LabelFilter) error {
	_, err := db.Exec(`
		UPDATE label_filters SET
			name = ?, sheet_id = ?, content_id = ?, print_mode = ?, search_term = ?, tags = ?, updated_at = datetime('now')
		WHERE id = ?`,
		f.Name, f.SheetID, f.ContentID, f.PrintMode, f.SearchTerm, f.Tags, f.ID)
	return err
}

func deleteLabelFilter(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM label_filters WHERE id = ?`, id)
	return err
}
