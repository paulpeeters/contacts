package main

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// handleLabelPrintGenerate builds the real batch labels PDF: no guide
// borders (unlike the content proof print), starting at the chosen label
// position. sheet_id, content_id, contact_id[], print_mode and start all
// come straight from the submitted form -- this never requires a saved
// LabelFilter, so an unsaved, one-off selection on the filter/print screen
// prints exactly the same way opening a previously saved filter does.
func handleLabelPrintGenerate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sheetID, err := strconv.ParseInt(r.FormValue("sheet_id"), 10, 64)
	if err != nil {
		http.Error(w, "kies een etikettenblad", http.StatusBadRequest)
		return
	}
	sheet, err := getLabelSheet(db, sheetID)
	if err != nil {
		http.Error(w, "etikettenblad niet gevonden", http.StatusBadRequest)
		return
	}

	contentID, err := strconv.ParseInt(r.FormValue("content_id"), 10, 64)
	if err != nil {
		http.Error(w, "kies een inhoud", http.StatusBadRequest)
		return
	}
	content, err := getLabelContent(db, contentID)
	if err != nil {
		http.Error(w, "inhoud niet gevonden", http.StatusBadRequest)
		return
	}

	idStrs := r.PostForm["contact_id[]"]
	if len(idStrs) == 0 {
		http.Error(w, "selecteer minstens één contact", http.StatusBadRequest)
		return
	}

	var contacts []LabelContact
	for _, idStr := range idStrs {
		cid, perr := strconv.ParseInt(idStr, 10, 64)
		if perr != nil {
			continue
		}
		c, gerr := getContactForLabels(db, cid)
		if gerr != nil {
			continue // contact may have been deleted since the page loaded
		}
		contacts = append(contacts, *c)
	}
	if len(contacts) == 0 {
		http.Error(w, "geen geldige contacten geselecteerd", http.StatusBadRequest)
		return
	}

	householdMode := r.FormValue("print_mode") == "household"
	if householdMode {
		contacts = dedupeByHousehold(contacts)
	}

	start := 1
	if v := strings.TrimSpace(r.FormValue("start")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			start = n
		}
	}
	perPage := sheet.Cols * sheet.Rows
	if perPage <= 0 {
		http.Error(w, "ongeldig raster: aantal etiketten horizontaal/verticaal moet groter dan 0 zijn", http.StatusBadRequest)
		return
	}
	startIndex := (start - 1) % perPage

	pdf, err := buildLabelsPDF(*sheet, *content, contacts, startIndex, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := pdf.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Etiketten-PDF gegenereerd: %d etiket(ten), inhoud %q, blad %q", len(contacts), content.Name, sheet.Name)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="etiketten-%s.pdf"`, sanitizeFilename(content.Name)))
	if err := pdf.Output(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleLabelChecklistGenerate builds the checklist PDF (A4 liggend,
// Volgnr/ID/Naam/Geschreven/Gestuurd/Ontvangen/Opmerkingen) for the same
// contact_id[]/print_mode selection as handleLabelPrintGenerate -- it
// doesn't need a sheet or content, since the checklist is always A4
// landscape regardless of what the labels themselves use. It's a separate
// endpoint, opened via its own button (see label_filter_form.html) rather
// than pages appended onto the labels PDF -- see the doc comment on
// buildChecklistPDF for why.
func handleLabelChecklistGenerate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	idStrs := r.PostForm["contact_id[]"]
	if len(idStrs) == 0 {
		http.Error(w, "selecteer minstens één contact", http.StatusBadRequest)
		return
	}

	var contacts []LabelContact
	for _, idStr := range idStrs {
		cid, perr := strconv.ParseInt(idStr, 10, 64)
		if perr != nil {
			continue
		}
		c, gerr := getContactForLabels(db, cid)
		if gerr != nil {
			continue
		}
		contacts = append(contacts, *c)
	}
	if len(contacts) == 0 {
		http.Error(w, "geen geldige contacten geselecteerd", http.StatusBadRequest)
		return
	}

	householdMode := r.FormValue("print_mode") == "household"
	if householdMode {
		contacts = dedupeByHousehold(contacts)
	}

	filterName := strings.TrimSpace(r.FormValue("name"))
	if filterName == "" {
		filterName = "ongenoemd filter"
	}
	title := fmt.Sprintf("Checklist etiketten %s (%s)", filterName, time.Now().Format("02-01-2006"))

	pdf := buildChecklistPDF(contacts, householdMode, title)
	if err := pdf.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Checklist-PDF gegenereerd: %d rij(en) (%s)", len(contacts), filterName)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="checklist.pdf"`)
	if err := pdf.Output(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// labelContentFitsSheet does a rough, position-only check of whether a
// content's elements are likely to fit within a sheet's computed label
// size -- it only looks at each element's starting X/Y, not the actual
// rendered text width/height (fpdf could tell us that, but only after
// setting the font for each element), so this is a heads-up, not a
// guarantee. Returns a human-readable warning, or "" if nothing looks
// obviously wrong.
func labelContentFitsSheet(sheet LabelSheet, content LabelContent) string {
	lw := sheet.LabelWidthMM()
	lh := sheet.LabelHeightMM()
	if lw <= 0 || lh <= 0 {
		return ""
	}
	for _, el := range content.Elements {
		if el.X < 0 || el.Y < 0 || el.X > lw || el.Y > lh {
			return fmt.Sprintf(
				"Let op: sommige elementen van inhoud \"%s\" staan buiten het etiket van etikettenblad \"%s\" (%.1f x %.1f mm) -- controleer de proefdruk.",
				content.Name, sheet.Name, lw, lh)
		}
	}
	return ""
}

// sanitizeFilename keeps a PDF download filename simple and safe (letters,
// digits, dash, underscore; spaces become dashes).
func sanitizeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "etiket"
	}
	return b.String()
}

// dedupeByHousehold collapses a list of selected contacts down to one
// synthetic, household-only LabelContact per distinct household -- so
// checking several members of the same family still prints just one label
// for that household, addressed with its shared fields. Personal fields
// (name, mobile, personal email, gender, birthdate, tags, last verified on,
// the contact ID) are deliberately left blank: with several members folded
// into one label there's no single "the" contact to show them for. Use
// {HouseholdID} instead of {ID} on a household-mode content if you need an
// identifier. Results are sorted by household label for a predictable,
// grouped print order.
func dedupeByHousehold(contacts []LabelContact) []LabelContact {
	seen := map[int64]bool{}
	var out []LabelContact
	for _, c := range contacts {
		if seen[c.HouseholdID] {
			continue
		}
		seen[c.HouseholdID] = true
		out = append(out, LabelContact{
			Contact:        Contact{HouseholdID: c.HouseholdID},
			HouseholdLabel: c.HouseholdLabel,
			Address:        c.Address,
			Zip:            c.Zip,
			City:           c.City,
			Country:        c.Country,
			Phone:          c.Phone,
			Email:          c.Email,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HouseholdLabel < out[j].HouseholdLabel })
	return out
}

// collectDistinctTags gathers the set of distinct, trimmed tags across all
// given contacts (tags are stored comma-separated per contact), sorted
// alphabetically for a stable filter UI.
func collectDistinctTags(contacts []LabelContact) []string {
	seen := map[string]bool{}
	for _, c := range contacts {
		for _, tag := range strings.Split(c.Tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				seen[tag] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
