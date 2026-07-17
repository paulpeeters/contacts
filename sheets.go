package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Label sheets: the reusable "physical paper" definition (paper size, the
// grid of labels on it, page margins, gap between labels) -- see
// LabelSheet's doc comment in models.go. Usually set up once per label
// sheet product you own, then reused by many different LabelContents.

func handleLabelSheetList(w http.ResponseWriter, r *http.Request) {
	sheets, err := listLabelSheets(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "label_sheet_list", sheets)
}

func handleLabelSheetNewForm(w http.ResponseWriter, r *http.Request) {
	render(w, "label_sheet_form", labelSheetFormData{
		Sheet: LabelSheet{
			PaperWidthMM:  210,
			PaperHeightMM: 297,
			Cols:          1,
			Rows:          1,
		},
	})
}

func handleLabelSheetEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s, err := getLabelSheet(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, "label_sheet_form", labelSheetFormData{Sheet: *s})
}

func handleLabelSheetCreate(w http.ResponseWriter, r *http.Request) {
	s, err := labelSheetFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.Name == "" {
		http.Error(w, "naam is verplicht", http.StatusBadRequest)
		return
	}
	id, err := createLabelSheet(db, s)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-sheets/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

func handleLabelSheetUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	s, err := labelSheetFromForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ID = id
	if s.Name == "" {
		http.Error(w, "naam is verplicht", http.StatusBadRequest)
		return
	}
	if err := updateLabelSheet(db, s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Redirect", fmt.Sprintf("/label-sheets/%d/edit", id))
	w.WriteHeader(http.StatusOK)
}

// handleLabelSheetDelete refuses to delete a sheet that's still referenced
// by a saved filter (mirrors the household-still-has-members guard) -- the
// filter would otherwise be left pointing at nothing.
func handleLabelSheetDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	n, err := countFiltersUsingSheet(db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Error(w, "dit etikettenblad wordt nog gebruikt door één of meer opgeslagen filters; pas die eerst aan", http.StatusBadRequest)
		return
	}
	if err := deleteLabelSheet(db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func labelSheetFromForm(r *http.Request) (LabelSheet, error) {
	var s LabelSheet
	if err := r.ParseForm(); err != nil {
		return s, err
	}

	s.Name = strings.TrimSpace(r.FormValue("name"))

	var err error
	if s.PaperWidthMM, err = parseFloatField(r, "paper_width_mm"); err != nil {
		return s, err
	}
	if s.PaperHeightMM, err = parseFloatField(r, "paper_height_mm"); err != nil {
		return s, err
	}
	if s.Cols, err = parseIntField(r, "cols"); err != nil {
		return s, err
	}
	if s.Rows, err = parseIntField(r, "label_rows"); err != nil {
		return s, err
	}
	if s.MarginLeftMM, err = parseFloatField(r, "margin_left_mm"); err != nil {
		return s, err
	}
	if s.MarginRightMM, err = parseFloatField(r, "margin_right_mm"); err != nil {
		return s, err
	}
	if s.MarginTopMM, err = parseFloatField(r, "margin_top_mm"); err != nil {
		return s, err
	}
	if s.MarginBottomMM, err = parseFloatField(r, "margin_bottom_mm"); err != nil {
		return s, err
	}
	if s.GapHMM, err = parseFloatField(r, "gap_h_mm"); err != nil {
		return s, err
	}
	if s.GapVMM, err = parseFloatField(r, "gap_v_mm"); err != nil {
		return s, err
	}

	return s, nil
}

// ---- shared form-parsing helpers (also used by contents.go) ---------------

// strAt safely indexes a string slice, returning "" if out of range.
func strAt(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return ""
	}
	return s[i]
}

func parseFloatField(r *http.Request, name string) (float64, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("ongeldige waarde voor %s: %v", name, err)
	}
	return f, nil
}

func parseIntField(r *http.Request, name string) (int, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("ongeldige waarde voor %s: %v", name, err)
	}
	return n, nil
}
