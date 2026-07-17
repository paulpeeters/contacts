package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Label contents: the reusable design of what goes on a single label (which
// fields sit where) -- see LabelContent's doc comment in models.go.
// Deliberately independent of any one LabelSheet; the sheet to preview
// against here is just a UI convenience (a dropdown, not saved).

// labelFieldOptions lists the Contact fields (plus the internal ID) that can
// be placed on a label.
var labelFieldOptions = []targetField{
	{"SeqNo", "Volgnummer (volgorde in deze afdruk)"},
	{"ID", "ID (persoonlijk)"},
	{"HouseholdID", "ID (huishouden)"},
	{"FirstName", "Voornaam"},
	{"LastName", "Achternaam"},
	{"HouseholdLabel", "Huishouden (aanhef op etiket)"},
	{"Address", "Adres"},
	{"Zip", "Postcode"},
	{"City", "Stad (HOOFDLETTERS)"},
	{"Country", "Land"},
	{"ForeignCountry", "Land (enkel indien niet eigen land, HOOFDLETTERS)"},
	{"Email", "E-mail (huishouden)"},
	{"PersonalEmail", "E-mail (persoonlijk)"},
	{"Phone", "Telefoon (huishouden)"},
	{"Mobile", "Mobiel (persoonlijk)"},
	{"Gender", "Geslacht"},
	{"Birthdate", "Geboortedatum"},
	{"Tags", "Tags"},
	{"LastVerifiedOn", "Laatst geverifieerd op"},
}

// fontOptions are the built-in PDF core fonts (no embedding needed, correct
// rendering of Dutch accented characters).
var fontOptions = []string{"Helvetica", "Times", "Courier"}

func handleLabelContentList(w http.ResponseWriter, r *http.Request) {
	contents, err := listLabelContents(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_content_list", contents)
}

func handleLabelContentNewForm(w http.ResponseWriter, r *http.Request) {
	sheets, err := listLabelSheets(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_content_form", labelContentFormData{
		Fields: labelFieldOptions,
		Fonts:  fontOptions,
		Sheets: sheets,
	})
}

func handleLabelContentEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := getLabelContent(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sheets, err := listLabelSheets(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_content_form", labelContentFormData{
		Content: *c,
		Fields:  labelFieldOptions,
		Fonts:   fontOptions,
		Sheets:  sheets,
	})
}

func handleLabelContentCreate(w http.ResponseWriter, r *http.Request) {
	c, err := labelContentFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if c.Name == "" {
		http.Error(w, "naam is verplicht", http.StatusBadRequest)
		return
	}
	id, err := createLabelContent(db, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-contents/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

func handleLabelContentUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := labelContentFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.ID = id
	if c.Name == "" {
		http.Error(w, "naam is verplicht", http.StatusBadRequest)
		return
	}
	if err := updateLabelContent(db, c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-contents/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

// handleLabelContentDelete refuses to delete a content that's still
// referenced by a saved filter (mirrors handleLabelSheetDelete).
func handleLabelContentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	n, err := countFiltersUsingContent(db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Error(w, "deze inhoud wordt nog gebruikt door één of meer opgeslagen filters; pas die eerst aan", http.StatusBadRequest)
		return
	}
	if err := deleteLabelContent(db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleLabelContentProof generates a proof-print PDF: this content filled
// with a fixed sample contact, printed onto the sheet given by ?sheet_id=
// (just for this one proof -- not saved anywhere), with light guide
// rectangles around each label. ?start=N (1-based) lets you start at any
// position on the sheet, e.g. when the first few labels are already used.
func handleLabelContentProof(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := getLabelContent(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	sheetID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("sheet_id")), 10, 64)
	if err != nil {
		http.Error(w, "kies een etikettenblad om tegen te proefdrukken", http.StatusBadRequest)
		return
	}
	s, err := getLabelSheet(db, sheetID)
	if err != nil {
		http.Error(w, "etikettenblad niet gevonden", http.StatusBadRequest)
		return
	}

	start := 1
	if v := strings.TrimSpace(r.URL.Query().Get("start")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			start = n
		}
	}

	perPage := s.Cols * s.Rows
	if perPage <= 0 {
		http.Error(w, "ongeldig raster: aantal etiketten horizontaal/verticaal moet groter dan 0 zijn", http.StatusBadRequest)
		return
	}
	startIndex := (start - 1) % perPage
	count := perPage - startIndex

	sample := sampleLabelContact()
	contacts := make([]LabelContact, count)
	for i := range contacts {
		contacts[i] = sample
	}

	pdf, err := buildLabelsPDF(*s, *c, contacts, startIndex, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := pdf.Error(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="proefdruk-%s.pdf"`, sanitizeFilename(c.Name)))
	if err := pdf.Output(w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// labelContentFromForm parses the padding settings and the element rows
// from a submitted form. Element rows use array-style field names
// (el_content[], el_x[], ...) so rows added/removed client-side via JS
// don't need server-tracked indices. Bold/Italic use <select> instead of
// checkboxes so every row always submits a value (unchecked checkboxes are
// simply omitted by browsers, which would misalign the parallel arrays).
func labelContentFromForm(r *http.Request) (LabelContent, error) {
	var c LabelContent
	if err := r.ParseForm(); err != nil {
		return c, err
	}

	c.Name = strings.TrimSpace(r.FormValue("name"))

	var err error
	if c.PaddingTopMM, err = parseFloatField(r, "padding_top_mm"); err != nil {
		return c, err
	}
	if c.PaddingRightMM, err = parseFloatField(r, "padding_right_mm"); err != nil {
		return c, err
	}
	if c.PaddingBottomMM, err = parseFloatField(r, "padding_bottom_mm"); err != nil {
		return c, err
	}
	if c.PaddingLeftMM, err = parseFloatField(r, "padding_left_mm"); err != nil {
		return c, err
	}

	contents := r.PostForm["el_content[]"]
	xs := r.PostForm["el_x[]"]
	ys := r.PostForm["el_y[]"]
	fonts := r.PostForm["el_font[]"]
	sizes := r.PostForm["el_size[]"]
	bolds := r.PostForm["el_bold[]"]
	italics := r.PostForm["el_italic[]"]

	for i := range contents {
		el := LabelElement{
			Content:    strAt(contents, i),
			FontFamily: strAt(fonts, i),
			Bold:       strAt(bolds, i) == "1",
			Italic:     strAt(italics, i) == "1",
		}
		el.X, _ = strconv.ParseFloat(strAt(xs, i), 64)
		el.Y, _ = strconv.ParseFloat(strAt(ys, i), 64)
		el.FontSize, _ = strconv.ParseFloat(strAt(sizes, i), 64)
		c.Elements = append(c.Elements, el)
	}

	return c, nil
}
