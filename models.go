package main

import "encoding/json"

// Contact is a single row in the contacts table: one person. Shared
// household data (address, common phone/email, card salutation) lives on
// Household instead -- see HouseholdID.
type Contact struct {
	ID             int64
	HouseholdID    int64
	FirstName      string
	LastName       string
	Gender         string
	Birthdate      string // ISO date, yyyy-mm-dd
	Mobile         string // personal mobile number
	Email          string // personal email address, distinct from Household.Email (shared)
	Tags           string // comma-separated, e.g. "family, work"
	LastVerifiedOn string // ISO date, yyyy-mm-dd
	CreatedAt      string
	UpdatedAt      string
}

// BirthYear returns just the year portion of Birthdate (empty if unset or
// too short to contain one) -- used by the /contacts and /households list
// filters to match a birth-year comparison (<, =, >) against yyyy-mm-dd
// dates.
func (c Contact) BirthYear() string {
	if len(c.Birthdate) < 4 {
		return ""
	}
	return c.Birthdate[:4]
}

// Household groups one or more contacts who share a mailing address (e.g.
// a couple or family) for the purposes of sending one card/label instead
// of one per person. Label is the free-text salutation used on the label
// itself (e.g. "Familie Peeters-Janssens"), separate from any individual
// contact's name.
type Household struct {
	ID        int64
	Label     string // card salutation, e.g. "Familie Peeters-Janssens"
	Address   string
	Zip       string
	City      string
	Country   string
	Phone     string // shared/landline phone, distinct from a contact's personal mobile
	Email     string // shared household email
	CreatedAt string
	UpdatedAt string
}

// ContactListRow is a Contact plus a couple of household fields joined in,
// for the contact list page.
type ContactListRow struct {
	Contact
	HouseholdLabel string
	City           string
	Address        string
}

// householdListRow is a Household plus its current members, for the
// households list page.
type householdListRow struct {
	Household
	Members []Contact
}

// memberFilterJSON is the shape MembersJSON emits per member -- deliberately
// short field names since this is embedded, one per member, directly into a
// data-members="..." HTML attribute on household_list.html and parsed back
// out client-side by the filter JS there.
type memberFilterJSON struct {
	FN   string `json:"fn"`
	LN   string `json:"ln"`
	Em   string `json:"em"`
	Tags string `json:"tags"`
	Yr   string `json:"yr"`
}

// MembersJSON returns this household's members (first/last name, personal
// email, tags, birth year) as a compact JSON array. household_list.html's
// filter needs to check "does at least one member match these personal-field
// criteria" -- rather than threading that logic through Go, the member data
// travels to the client as JSON and the matching happens there, reusing the
// same per-field match functions the /contacts page already has.
func (r householdListRow) MembersJSON() string {
	items := make([]memberFilterJSON, 0, len(r.Members))
	for _, m := range r.Members {
		items = append(items, memberFilterJSON{FN: m.FirstName, LN: m.LastName, Em: m.Email, Tags: m.Tags, Yr: m.BirthYear()})
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// householdFormData is what the household add/edit template renders.
type householdFormData struct {
	Household Household
	Members   []Contact
}

// formData is what the add/edit template renders. HouseholdID on a
// zero-value Contact can be pre-filled (see handleNewForm's household_id
// query param) so "+ Nieuw contact in dit huishouden" on the household edit
// page lands with that household already selected. AllTags is every
// distinct tag already used by any contact, offered as a checkbox picker
// alongside the free-text tags field.
type formData struct {
	Contact    Contact
	Households []Household // for the "which household" picker
	AllTags    []string
}

// contactsListData is what the contacts_list template renders. AllTags is
// every distinct tag already used by any contact, offered as the filter
// panel's tag picker.
type contactsListData struct {
	Contacts          []ContactListRow
	AllTags           []string
	ShowSyncSummary   bool
	ContactsCreated   int
	ContactsUpdated   int
	HouseholdsCreated int
	HouseholdsUpdated int
}

// householdListData is what the households list template renders. AllTags
// mirrors contactsListData.AllTags, for the same tag-filter picker on this page.
type householdListData struct {
	Households []householdListRow
	AllTags    []string
}

// settingsData is what the settings page renders.
type settingsData struct {
	HomeCountry string
}

// homeData is what the home page renders. Besides the version banner, it
// drives the database-chooser modal: ShowDBChooser opens it automatically
// (once per process run, the first time /home loads -- see handleHome),
// DBError/DBAttempt re-open it with an error message + the path that was
// tried after a failed switch (see handleChooseDBSubmit in dbchooser.go),
// and ShowDBSwitchedBanner shows a one-time success banner after a switch.
type homeData struct {
	AppVersion           string
	DBPath               string
	ShowDBChooser        bool
	DBError              string
	DBAttempt            string
	ShowDBSwitchedBanner bool
	DBCreated            bool // true if ShowDBSwitchedBanner is for a newly-created db, not an existing one being switched to

	// Backup* drive the one-time banner shown after a "Back-up maken" click
	// (see handleBackupDB in backup.go) -- BackupPath is the new backup
	// file's path on success, BackupError is set instead on failure.
	ShowBackupBanner bool
	BackupPath       string
	BackupError      string
}

// LabelElement is one line of content placed on a label, positioned at
// (X, Y) mm from the label's top-left corner (Y is the text baseline).
// Content is free text that may mix static text with {FieldName}
// placeholders (e.g. "{FirstName} {LastName}" or "Tel: {Phone}"), so
// multiple fields can be combined on a single line before being placed.
type LabelElement struct {
	Content    string  `json:"content"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	FontFamily string  `json:"font_family"` // "Helvetica", "Times", "Courier"
	FontSize   float64 `json:"font_size"`   // pt
	Bold       bool    `json:"bold"`
	Italic     bool    `json:"italic"`
}

// LabelSheet is a reusable label sheet definition: paper size, the grid of
// labels on it, the page margins and the gap between labels. Usually set up
// once per physical sheet product you buy (e.g. "A4, 3 x 8 labels"), then
// reused by any number of different LabelContents -- the sheet and the
// content placed on it are deliberately independent (see LabelContent).
type LabelSheet struct {
	ID             int64
	Name           string
	PaperWidthMM   float64
	PaperHeightMM  float64
	Cols           int
	Rows           int
	MarginLeftMM   float64
	MarginRightMM  float64
	MarginTopMM    float64
	MarginBottomMM float64
	GapHMM         float64
	GapVMM         float64
	CreatedAt      string
	UpdatedAt      string
}

// LabelWidthMM returns the computed width of a single label, derived from
// the paper width, side margins, column count and horizontal gap.
func (s LabelSheet) LabelWidthMM() float64 {
	if s.Cols <= 0 {
		return 0
	}
	return (s.PaperWidthMM - s.MarginLeftMM - s.MarginRightMM - float64(s.Cols-1)*s.GapHMM) / float64(s.Cols)
}

// LabelHeightMM returns the computed height of a single label, derived from
// the paper height, top/bottom margins, row count and vertical gap.
func (s LabelSheet) LabelHeightMM() float64 {
	if s.Rows <= 0 {
		return 0
	}
	return (s.PaperHeightMM - s.MarginTopMM - s.MarginBottomMM - float64(s.Rows-1)*s.GapVMM) / float64(s.Rows)
}

// LabelContent is a reusable design for what goes on a single label: which
// fields sit where. Deliberately not tied to one specific LabelSheet -- the
// same content can be used with any sheet whose label is large enough to
// fit it (checked, loosely, at print time -- see labelContentFitsSheet in
// print.go -- not enforced here). PaddingXxxMM is a visual safe-area guide
// only, used while designing the content; it isn't enforced when printing.
type LabelContent struct {
	ID              int64
	Name            string
	PaddingTopMM    float64
	PaddingRightMM  float64
	PaddingBottomMM float64
	PaddingLeftMM   float64
	Elements        []LabelElement
	CreatedAt       string
	UpdatedAt       string
}

// MaxElementX returns the rightmost X position among this content's
// elements (0 if it has none). Used, together with MaxElementY, for a
// rough client-side "does this fit on that sheet" heads-up on the print
// screen (label_filter_form.html) -- see labelContentFitsSheet in print.go
// for the equivalent server-side check.
func (c LabelContent) MaxElementX() float64 {
	var max float64
	for _, el := range c.Elements {
		if el.X > max {
			max = el.X
		}
	}
	return max
}

// MaxElementY is MaxElementX's vertical equivalent.
func (c LabelContent) MaxElementY() float64 {
	var max float64
	for _, el := range c.Elements {
		if el.Y > max {
			max = el.Y
		}
	}
	return max
}

// LabelFilter is a saved, reusable contact/household selection: a search
// term and tags to pre-apply, a print mode, and which LabelSheet +
// LabelContent to print with. Saving one just remembers this criteria for
// quick recall later -- the actual set of checked contacts on the print
// screen is always re-derived from the search/tags (and still freely
// adjustable there), never stored itself.
type LabelFilter struct {
	ID         int64
	Name       string
	SheetID    int64
	ContentID  int64
	PrintMode  string // "contact" or "household"
	SearchTerm string
	// Tags holds the tri-state tag filter from label_filter_form.html, as
	// "tag:state|tag:state" pairs (state 1 = must-have, 2 = must-not-have) --
	// entirely opaque to Go, parsed/built client-side (see that template's
	// parseSavedTags). A tag with no ":state" suffix (the plain
	// comma-separated format this column used before tri-state existed) is
	// treated as state 1, so old saved filters still restore correctly.
	Tags      string
	TagsMode  string // "and" or "or" -- how the Tags conditions combine
	CreatedAt string
	UpdatedAt string
}

// labelSheetFormData is what the label sheet add/edit template renders.
type labelSheetFormData struct {
	Sheet LabelSheet
}

// labelContentFormData is what the label content add/edit template renders.
// Sheets is the full list of saved sheets, offered purely as a "preview
// against" picker while designing -- not saved as part of the content.
type labelContentFormData struct {
	Content LabelContent
	Fields  []targetField
	Fonts   []string
	Sheets  []LabelSheet
}

// LabelContact is a Contact with its household's shared fields (address,
// shared phone/email, card salutation label) joined in flat, so label
// elements can reference either a personal field (e.g. {FirstName}) or a
// household field (e.g. {Address}, {HouseholdLabel}) the same way.
//
// Note: Email here is the household's shared email and deliberately shadows
// the embedded Contact.Email (the person's own address) -- use c.Email for
// the household one and c.Contact.Email for the personal one. The
// {PersonalEmail} vs {Email} label placeholders (see pdf.go) mirror this,
// the same way {Mobile} (personal) and {Phone} (household) already do.
type LabelContact struct {
	Contact
	HouseholdLabel string
	Address        string
	Zip            string
	City           string
	Country        string
	Phone          string // shared/landline household phone
	Email          string // shared household email
}

// labelFilterFormData is what the label filter add/edit + contact
// selection/print template renders. Filter may be a brand new, not-yet-saved
// one (ID == 0) -- printing never requires a saved filter, only Sheets,
// Contents and Contacts to choose from and act on.
type labelFilterFormData struct {
	Filter   LabelFilter
	Sheets   []LabelSheet
	Contents []LabelContent
	Contacts []LabelContact
	Tags     []string
}

// labelFilterListData is what the label filter list template renders.
// SheetNames/ContentNames map a LabelFilter's SheetID/ContentID to their
// human names, so the list doesn't have to show raw IDs.
type labelFilterListData struct {
	Filters      []LabelFilter
	SheetNames   map[int64]string
	ContentNames map[int64]string
}
