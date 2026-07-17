package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Label filters: a saved, reusable contact/household selection (search
// term, tags, print mode) plus which sheet + content to print with -- see
// LabelFilter's doc comment in models.go. The same screen (label_filter_form)
// doubles as both the filter editor and the contact-selection/print screen:
// saving a filter is optional (via "Filter bewaren"), printing never
// requires one to be saved first.

func handleLabelFilterList(w http.ResponseWriter, r *http.Request) {
	filters, err := listLabelFilters(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sheets, err := listLabelSheets(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	contents, err := listLabelContents(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sheetNames := map[int64]string{}
	for _, s := range sheets {
		sheetNames[s.ID] = s.Name
	}
	contentNames := map[int64]string{}
	for _, c := range contents {
		contentNames[c.ID] = c.Name
	}
	render(w, "label_filter_list", labelFilterListData{
		Filters:      filters,
		SheetNames:   sheetNames,
		ContentNames: contentNames,
	})
}

func handleLabelFilterNewForm(w http.ResponseWriter, r *http.Request) {
	data, err := labelFilterFormDataFor(LabelFilter{PrintMode: "contact"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_filter_form", data)
}

func handleLabelFilterEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	f, err := getLabelFilter(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := labelFilterFormDataFor(*f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_filter_form", data)
}

// labelFilterFormDataFor assembles everything the filter form / contact
// selection+print screen needs to render: the filter itself (possibly a
// brand new, not-yet-saved one), every saved sheet and content to choose
// from, and every contact plus the set of distinct tags for the
// search/tag-filter UI.
func labelFilterFormDataFor(f LabelFilter) (labelFilterFormData, error) {
	sheets, err := listLabelSheets(db)
	if err != nil {
		return labelFilterFormData{}, err
	}
	contents, err := listLabelContents(db)
	if err != nil {
		return labelFilterFormData{}, err
	}
	contacts, err := listContactsForLabels(db)
	if err != nil {
		return labelFilterFormData{}, err
	}
	return labelFilterFormData{
		Filter:   f,
		Sheets:   sheets,
		Contents: contents,
		Contacts: contacts,
		Tags:     collectDistinctTags(contacts),
	}, nil
}

func handleLabelFilterCreate(w http.ResponseWriter, r *http.Request) {
	f, err := labelFilterFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if f.Name == "" {
		http.Error(w, "naam is verplicht om een filter te bewaren", http.StatusBadRequest)
		return
	}
	id, err := createLabelFilter(db, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-filters/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

func handleLabelFilterUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	f, err := labelFilterFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.ID = id
	if f.Name == "" {
		http.Error(w, "naam is verplicht om een filter te bewaren", http.StatusBadRequest)
		return
	}
	if err := updateLabelFilter(db, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-filters/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

func handleLabelFilterDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := deleteLabelFilter(db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// labelFilterFromForm parses the name/sheet/content/print-mode/search/tags
// criteria a filter saves. Deliberately doesn't touch contact_id[] -- the
// actual checked contacts are never part of a saved filter, only the
// criteria used to (re-)select them (see LabelFilter's doc comment).
func labelFilterFromForm(r *http.Request) (LabelFilter, error) {
	var f LabelFilter
	if err := r.ParseForm(); err != nil {
		return f, err
	}

	f.Name = strings.TrimSpace(r.FormValue("name"))

	sheetID, err := strconv.ParseInt(r.FormValue("sheet_id"), 10, 64)
	if err != nil {
		return f, fmt.Errorf("kies een etikettenblad")
	}
	f.SheetID = sheetID

	contentID, err := strconv.ParseInt(r.FormValue("content_id"), 10, 64)
	if err != nil {
		return f, fmt.Errorf("kies een inhoud")
	}
	f.ContentID = contentID

	f.PrintMode = r.FormValue("print_mode")
	if f.PrintMode != "household" {
		f.PrintMode = "contact"
	}
	f.SearchTerm = strings.TrimSpace(r.FormValue("search_term"))
	f.Tags = strings.Join(r.PostForm["tags[]"], ",")

	return f, nil
}
