package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// syncColumns is the fixed set of column headers used by both the Excel
// export and the sync re-import. Sync is a round-trip of our own export, so
// it looks columns up by exact name (case/whitespace-insensitive) rather
// than fuzzy-matching arbitrary spreadsheet headers.
var syncColumns = []string{
	"ContactID", "HouseholdID", "FirstName", "LastName", "Gender", "Birthdate",
	"Mobile", "PersonalEmail", "Tags", "LastVerifiedOn",
	"HouseholdLabel", "Address", "Zip", "City", "Country", "Phone", "Email",
}

// syncDateColumns are the syncColumns holding an ISO yyyy-mm-dd date as plain
// text. Excel "helpfully" reparses and reformats a General-format cell that
// looks like a date the moment you edit (or retype) it -- using your
// Windows/Excel regional short-date setting, e.g. dd/mm/yyyy -- which risks
// day/month ambiguity on the way back in. Forcing these columns to the "@"
// (Text) format (see markDateColumnsAsText) keeps whatever you actually typed
// as literal text, so what you see is exactly what normalizeDate parses back.
var syncDateColumns = []string{"Birthdate", "LastVerifiedOn"}

// syncColumnLetter returns the Excel column letter (e.g. "F") for a
// syncColumns entry by name.
func syncColumnLetter(name string) (string, error) {
	for i, col := range syncColumns {
		if col == name {
			return excelize.ColumnNumberToName(i + 1)
		}
	}
	return "", fmt.Errorf("onbekende sync-kolom %q", name)
}

// markDateColumnsAsText sets the "@" (Text) number format on syncDateColumns,
// for the whole column -- covering both the rows already written and any row
// a user adds later in Excel -- so a date like "2026-01-10" is never silently
// reinterpreted/reformatted. See syncDateColumns' doc comment for why.
func markDateColumnsAsText(f *excelize.File, sheet string) error {
	textFmt := "@"
	styleID, err := f.NewStyle(&excelize.Style{CustomNumFmt: &textFmt})
	if err != nil {
		return err
	}
	for _, col := range syncDateColumns {
		letter, err := syncColumnLetter(col)
		if err != nil {
			return err
		}
		if err := f.SetColStyle(sheet, letter, styleID); err != nil {
			return err
		}
	}
	return nil
}

// targetField is a (field name, human label) pair. Originally introduced for
// the now-removed generic Excel import's column-mapping dropdowns; still
// used by the label content field picker (see labelFieldOptions in
// contents.go).
type targetField struct {
	Value string
	Label string
}

// newImportID generates a random hex id used to key an in-memory pending
// plan (see pendingSync below) between an upload's preview and confirm
// steps.
func newImportID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// importDateLayouts are the date formats normalizeDate tries, Dutch
// dd-mm-yyyy first.
var importDateLayouts = []string{
	"2006-01-02",
	"02-01-2006",
	"02/01/2006",
	"2-1-2006",
	"2/1/2006",
	"01/02/2006",
	"2006/01/02",
	"02-Jan-2006",
	"2 January 2006",
	"January 2, 2006",
}

// normalizeDate tries a handful of common layouts (Dutch dd-mm-yyyy first)
// and converts to ISO yyyy-mm-dd. If nothing matches, the original value is
// kept so data isn't silently lost.
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range importDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return s
}

// householdLabelOrDefault falls back to "FirstName LastName" as the card
// salutation when no household label was given (or it was blank).
func householdLabelOrDefault(h Household, c Contact) string {
	if strings.TrimSpace(h.Label) != "" {
		return h.Label
	}
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}

// contactDataEqual reports whether two Contacts have identical field values
// as far as sync is concerned (ignoring ID/HouseholdID/CreatedAt/UpdatedAt --
// household membership is compared separately since it needs the target
// HouseholdID, not the Contact struct).
func contactDataEqual(a, b Contact) bool {
	return a.FirstName == b.FirstName &&
		a.LastName == b.LastName &&
		a.Gender == b.Gender &&
		a.Birthdate == b.Birthdate &&
		a.Mobile == b.Mobile &&
		a.Email == b.Email &&
		a.Tags == b.Tags &&
		a.LastVerifiedOn == b.LastVerifiedOn
}

// householdDataEqual reports whether two Households have identical field
// values as far as sync is concerned (ignoring ID/CreatedAt/UpdatedAt).
func householdDataEqual(a, b Household) bool {
	return a.Label == b.Label &&
		a.Address == b.Address &&
		a.Zip == b.Zip &&
		a.City == b.City &&
		a.Country == b.Country &&
		a.Phone == b.Phone &&
		a.Email == b.Email
}

// normalizeHeader lowercases, strips common accents and collapses any
// non-alphanumeric run into a single space. Originally written to make
// spreadsheet column headers compare equal regardless of accents/casing for
// the now-removed generic import; still used by pdf.go's {ForeignCountry}
// placeholder to compare the configured home country against a household's
// country accent- and case-insensitively.
func normalizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"á", "a", "à", "a", "â", "a",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
	s = replacer.Replace(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// ---- export --------------------------------------------------------------

// handleContactExport streams an .xlsx with one row per contact, all
// personal fields plus its household's shared fields flattened in (so a
// household with several members appears duplicated across their rows --
// that's expected, see the sync upload page for why). ContactID and
// HouseholdID are included so the file can be edited and synced back.
func handleContactExport(w http.ResponseWriter, r *http.Request) {
	contacts, err := listContactsForExport(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Contacten"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	header := make([]interface{}, len(syncColumns))
	for i, h := range syncColumns {
		header[i] = h
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for i, lc := range contacts {
		row := []interface{}{
			lc.ID, lc.HouseholdID, lc.FirstName, lc.LastName, lc.Gender, lc.Birthdate,
			lc.Mobile, lc.Contact.Email, lc.Tags, lc.LastVerifiedOn,
			lc.HouseholdLabel, lc.Address, lc.Zip, lc.City, lc.Country, lc.Phone, lc.Email,
		}
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := markDateColumnsAsText(f, sheet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Contacten geëxporteerd naar Excel: %d rij(en)", len(contacts))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="contacten-export.xlsx"`)
	if err := f.Write(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleSyncSample streams a sample .xlsx with the exact header row sync
// expects, plus two example contacts sharing one household (both ContactID
// and HouseholdID set to 0, with identical household columns) -- so a user
// starting from scratch can see the expected format without first having to
// export existing data. Since both rows have HouseholdID 0 and matching
// household fields, syncing this file as-is creates one shared household for
// both contacts (see the grouping logic in handleSyncPreview/handleSyncConfirm).
func handleSyncSample(w http.ResponseWriter, r *http.Request) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Contacten"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	header := make([]interface{}, len(syncColumns))
	for i, h := range syncColumns {
		header[i] = h
	}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sampleRows := [][]interface{}{
		{0, 0, "Jan", "Janssens", "Male", "1980-05-14", "0475 12 34 56", "jan.persoonlijk@example.com", "vriend",
			"2026-01-10", "Familie Janssens-Peeters", "Kerkstraat 12", "9000", "Gent", "België", "09 123 45 67", "familie.janssens@example.com"},
		{0, 0, "Marie", "Peeters", "Female", "1982-03-02", "0475 65 43 21", "marie.persoonlijk@example.com", "vriend",
			"2026-01-10", "Familie Janssens-Peeters", "Kerkstraat 12", "9000", "Gent", "België", "09 123 45 67", "familie.janssens@example.com"},
	}
	for i, row := range sampleRows {
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if err := markDateColumnsAsText(f, sheet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="contacten-sync-voorbeeld.xlsx"`)
	if err := f.Write(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// ---- sync: pending-plan store ---------------------------------------------
// The parsed, validated plan is kept in memory between the preview and
// confirm steps, keyed by a random id carried in a hidden form field.

type syncRowPlan struct {
	RowNum         int // 1-based spreadsheet row, for messages
	ContactID      int64
	HouseholdID    int64
	IsNewContact   bool
	IsNewHousehold bool
	Contact        Contact
	Household      Household
	// NewHouseholdGroup is only meaningful when IsNewHousehold: rows whose
	// HouseholdID is 0 *and* whose household fields (label/address/zip/city/
	// country/phone/email) are byte-for-byte identical share the same group
	// number, so handleSyncConfirm creates one household for the group
	// instead of one per row. See the grouping pass in handleSyncPreview.
	NewHouseholdGroup int
	// ContactChanged/HouseholdChanged are only meaningful when
	// IsNewContact/IsNewHousehold is false: they say whether this row's data
	// actually differs from what's currently in the database, so a sync of
	// an unmodified export doesn't show every single row as "bijwerken" --
	// only ones that will really change something. handleSyncConfirm also
	// skips the UPDATE entirely when false, so re-syncing an unchanged row
	// doesn't even bump updated_at.
	ContactChanged   bool
	HouseholdChanged bool
}

type pendingSync struct {
	Rows                []syncRowPlan
	Errors              []string // blocking: confirm is refused while any exist
	Warnings            []string // informational only
	NewContacts         int
	UpdatedContacts     int
	UnchangedContacts   int
	NewHouseholds       int
	UpdatedHouseholds   int
	UnchangedHouseholds int
	Created             time.Time
}

var (
	pendingSyncsMu sync.Mutex
	pendingSyncs   = map[string]*pendingSync{}
)

func storePendingSync(p *pendingSync) string {
	id := newImportID()
	pendingSyncsMu.Lock()
	defer pendingSyncsMu.Unlock()
	for k, v := range pendingSyncs {
		if time.Since(v.Created) > time.Hour {
			delete(pendingSyncs, k)
		}
	}
	pendingSyncs[id] = p
	return id
}

func takePendingSync(id string) *pendingSync {
	pendingSyncsMu.Lock()
	defer pendingSyncsMu.Unlock()
	p := pendingSyncs[id]
	delete(pendingSyncs, id)
	return p
}

// syncPreviewData is what the sync preview template renders.
type syncPreviewData struct {
	SyncID string
	*pendingSync
}

// ---- sync: handlers --------------------------------------------------------

func handleSyncForm(w http.ResponseWriter, r *http.Request) {
	render(w, "sync_upload", nil)
}

// syncHeaderIndex maps each expected column name (case/whitespace
// insensitive) to its position in the uploaded header row, and reports any
// expected columns that are missing entirely.
func syncHeaderIndex(headerRow []string) (map[string]int, []string) {
	idx := make(map[string]int, len(headerRow))
	for i, h := range headerRow {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var missing []string
	for _, col := range syncColumns {
		if _, ok := idx[strings.ToLower(col)]; !ok {
			missing = append(missing, col)
		}
	}
	return idx, missing
}

func syncCell(row []string, idx map[string]int, col string) string {
	i, ok := idx[strings.ToLower(col)]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// parseSyncID parses a ContactID/HouseholdID cell. An empty cell or literal
// "0" means "create new"; anything that isn't a valid non-negative integer
// is reported back via ok=false.
func parseSyncID(s string) (id int64, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// handleSyncPreview parses an uploaded export (or an edited copy of one),
// validates every row and builds an in-memory plan without writing
// anything to the database yet -- that only happens on confirm.
func handleSyncPreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "kon bestand niet lezen: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "geen bestand ontvangen", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if ext := strings.ToLower(filepath.Ext(fileHeader.Filename)); ext != ".xlsx" {
		http.Error(w, "alleen .xlsx bestanden worden ondersteund", http.StatusBadRequest)
		return
	}

	f, err := excelize.OpenReader(file)
	if err != nil {
		http.Error(w, "kon Excel-bestand niet openen: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		http.Error(w, "geen werkblad gevonden in het bestand", http.StatusBadRequest)
		return
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		http.Error(w, "kon rijen niet lezen: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(rows) < 1 {
		http.Error(w, "het bestand lijkt leeg te zijn", http.StatusBadRequest)
		return
	}

	headerIdx, missing := syncHeaderIndex(rows[0])
	if len(missing) > 0 {
		http.Error(w, "dit lijkt niet op een bestand van 'Exporteren naar Excel': volgende kolommen ontbreken: "+
			strings.Join(missing, ", "), http.StatusBadRequest)
		return
	}

	plan := &pendingSync{Created: time.Now()}
	seenContactIDs := map[int64]int{}      // ContactID -> first row number seen
	seenHouseholds := map[int64]Household{} // HouseholdID -> field values first seen
	householdFirstRow := map[int64]int{}
	// Groups rows with HouseholdID == 0 by identical household field values,
	// so e.g. two new contacts on the same new household (a couple) sync to
	// one shared household instead of two separate ones -- see
	// syncRowPlan.NewHouseholdGroup and its use in handleSyncConfirm.
	newHouseholdGroups := map[Household]int{}
	nextNewHouseholdGroup := 1

	for i, row := range rows[1:] {
		rowNum := i + 2 // 1-based, header is row 1
		get := func(col string) string { return syncCell(row, headerIdx, col) }

		empty := true
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}

		firstName := get("FirstName")
		lastName := get("LastName")
		if firstName == "" && lastName == "" {
			continue // blank person row, ignore
		}
		if firstName == "" || lastName == "" {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Rij %d: voornaam en achternaam zijn beide verplicht", rowNum))
			continue
		}

		contactID, ok := parseSyncID(get("ContactID"))
		if !ok {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Rij %d: ongeldige ContactID", rowNum))
			continue
		}
		householdID, ok := parseSyncID(get("HouseholdID"))
		if !ok {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Rij %d: ongeldige HouseholdID", rowNum))
			continue
		}

		var existingContact *Contact
		if contactID != 0 {
			ec, err := getContact(db, contactID)
			if err != nil {
				plan.Errors = append(plan.Errors, fmt.Sprintf("Rij %d: ContactID %d bestaat niet", rowNum, contactID))
				continue
			}
			existingContact = ec
			if firstRow, dup := seenContactIDs[contactID]; dup {
				plan.Errors = append(plan.Errors, fmt.Sprintf(
					"Rij %d: ContactID %d komt ook al voor op rij %d (dubbel)", rowNum, contactID, firstRow))
				continue
			}
			seenContactIDs[contactID] = rowNum
		}
		var existingHousehold *Household
		if householdID != 0 {
			eh, err := getHousehold(db, householdID)
			if err != nil {
				plan.Errors = append(plan.Errors, fmt.Sprintf("Rij %d: HouseholdID %d bestaat niet", rowNum, householdID))
				continue
			}
			existingHousehold = eh
		}

		c := Contact{
			FirstName:      firstName,
			LastName:       lastName,
			Gender:         get("Gender"),
			Birthdate:      normalizeDate(get("Birthdate")),
			Mobile:         get("Mobile"),
			Email:          get("PersonalEmail"),
			Tags:           get("Tags"),
			LastVerifiedOn: normalizeDate(get("LastVerifiedOn")),
		}

		h := Household{
			Label:   get("HouseholdLabel"),
			Address: get("Address"),
			Zip:     get("Zip"),
			City:    get("City"),
			Country: get("Country"),
			Phone:   get("Phone"),
			Email:   get("Email"),
		}
		// A household always needs some non-empty label (it's the card
		// salutation): fall back to "Firstname Lastname" of whichever
		// contact happens to be on this row if the sheet left it blank,
		// same fallback the generic import uses.
		h.Label = householdLabelOrDefault(h, c)

		var newGroup int
		if householdID != 0 {
			if prev, seen := seenHouseholds[householdID]; seen && prev != h {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf(
					"HouseholdID %d heeft verschillende gegevens op rij %d en rij %d; rij %d (laatste) wordt gebruikt",
					householdID, householdFirstRow[householdID], rowNum, rowNum))
			}
			if _, seen := seenHouseholds[householdID]; !seen {
				householdFirstRow[householdID] = rowNum
			}
			seenHouseholds[householdID] = h
		} else {
			// h's ID/CreatedAt/UpdatedAt are all zero-valued here (never set
			// above), so two rows produce an equal Household value -- and thus
			// the same map key -- exactly when every household field they
			// filled in matches byte-for-byte.
			if g, ok := newHouseholdGroups[h]; ok {
				newGroup = g
			} else {
				newGroup = nextNewHouseholdGroup
				newHouseholdGroups[h] = newGroup
				nextNewHouseholdGroup++
			}
		}

		// A row only really needs writing if its data differs from what's
		// already in the database -- comparing against existingContact/
		// existingHousehold (both nil for brand-new rows, where "changed"
		// trivially holds since there's nothing yet to match). For contacts,
		// existingContact.HouseholdID != householdID also counts as a change
		// (the row moves the contact to a different household) -- this
		// naturally also covers householdID == 0 (a new household), since an
		// existing contact's HouseholdID is never 0.
		contactChanged := true
		if existingContact != nil {
			contactChanged = !contactDataEqual(c, *existingContact) || existingContact.HouseholdID != householdID
		}
		householdChanged := true
		if existingHousehold != nil {
			householdChanged = !householdDataEqual(h, *existingHousehold)
		}

		plan.Rows = append(plan.Rows, syncRowPlan{
			RowNum:            rowNum,
			ContactID:         contactID,
			HouseholdID:       householdID,
			IsNewContact:      contactID == 0,
			IsNewHousehold:    householdID == 0,
			Contact:           c,
			Household:         h,
			NewHouseholdGroup: newGroup,
			ContactChanged:    contactChanged,
			HouseholdChanged:  householdChanged,
		})
	}

	// Households can be referenced (by an existing HouseholdID) from several
	// rows, so count distinct household ids rather than rows -- a household
	// only counts as "changed" if at least one referencing row disagrees
	// with what's currently stored (an "unchanged" verdict from every row
	// referencing it is required to count it as unchanged).
	changedHouseholds := map[int64]bool{}
	unchangedHouseholds := map[int64]bool{}
	for _, rp := range plan.Rows {
		if rp.IsNewContact {
			plan.NewContacts++
		} else if rp.ContactChanged {
			plan.UpdatedContacts++
		} else {
			plan.UnchangedContacts++
		}
		if !rp.IsNewHousehold {
			if rp.HouseholdChanged {
				changedHouseholds[rp.HouseholdID] = true
			} else {
				unchangedHouseholds[rp.HouseholdID] = true
			}
		}
	}
	for id := range changedHouseholds {
		delete(unchangedHouseholds, id)
	}
	plan.NewHouseholds = len(newHouseholdGroups)
	plan.UpdatedHouseholds = len(changedHouseholds)
	plan.UnchangedHouseholds = len(unchangedHouseholds)

	id := storePendingSync(plan)
	render(w, "sync_preview", syncPreviewData{SyncID: id, pendingSync: plan})
}

// handleSyncConfirm applies a previously validated plan: households first
// (so new households have an id before their members are written), then
// contacts. Refuses to run if the plan has any blocking errors -- the
// preview page shouldn't offer a confirm button in that case, but this is
// checked again here regardless.
func handleSyncConfirm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	plan := takePendingSync(r.FormValue("sync_id"))
	if plan == nil {
		http.Error(w, "deze sync is verlopen, upload het bestand opnieuw", http.StatusBadRequest)
		return
	}
	if len(plan.Errors) > 0 {
		http.Error(w, "deze sync bevat fouten en kan niet bevestigd worden", http.StatusBadRequest)
		return
	}

	// Rows sharing a NewHouseholdGroup (identical household fields, both
	// HouseholdID == 0) get exactly one household created for the group --
	// this map remembers, per group, the id assigned to the first row in it
	// so later rows in the same group reuse it instead of creating another.
	createdHouseholds := map[int]int64{}

	for _, rp := range plan.Rows {
		var hid int64
		if rp.IsNewHousehold {
			if existingID, ok := createdHouseholds[rp.NewHouseholdGroup]; ok {
				hid = existingID
			} else {
				newID, err := createHousehold(db, rp.Household)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				hid = newID
				createdHouseholds[rp.NewHouseholdGroup] = newID
			}
		} else {
			hid = rp.HouseholdID
			// Skip the write entirely for a household this row didn't
			// actually change -- besides avoiding a no-op UPDATE, it means
			// re-syncing an untouched export doesn't bump every household's
			// updated_at.
			if rp.HouseholdChanged {
				h := rp.Household
				h.ID = hid
				if err := updateHousehold(db, h); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}

		if rp.IsNewContact {
			c := rp.Contact
			c.HouseholdID = hid
			if _, err := createContact(db, c); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if rp.ContactChanged {
			c := rp.Contact
			c.ID = rp.ContactID
			c.HouseholdID = hid
			if err := updateContact(db, c); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// else: existing contact, nothing about it (including its household)
		// actually changed -- skip the write, same reasoning as households
		// above.
	}

	log.Printf(
		"Sync bevestigd: contacten %d nieuw / %d bijgewerkt / %d ongewijzigd, huishoudens %d nieuw / %d bijgewerkt / %d ongewijzigd",
		plan.NewContacts, plan.UpdatedContacts, plan.UnchangedContacts,
		plan.NewHouseholds, plan.UpdatedHouseholds, plan.UnchangedHouseholds)
	http.Redirect(w, r, fmt.Sprintf(
		"/contacts?contacts_created=%d&contacts_updated=%d&households_created=%d&households_updated=%d",
		plan.NewContacts, plan.UpdatedContacts, plan.NewHouseholds, plan.UpdatedHouseholds), http.StatusFound)
}
