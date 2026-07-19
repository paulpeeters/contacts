package main

import (
	"log"
	"net/http"
	"strconv"
	"strings"
)

func handleHouseholdList(w http.ResponseWriter, r *http.Request) {
	rows, err := listHouseholdsWithMembers(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, err := listDistinctContactTags(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "household_list", householdListData{Households: rows, AllTags: tags})
}

func handleHouseholdNewForm(w http.ResponseWriter, r *http.Request) {
	render(w, "household_form", householdFormData{})
}

func handleHouseholdEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	h, err := getHousehold(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	members, err := listHouseholdMembers(db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "household_form", householdFormData{Household: *h, Members: members})
}

func handleHouseholdCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h := householdFromForm(r)
	if h.Label == "" {
		http.Error(w, "aanhef/label is verplicht", http.StatusBadRequest)
		return
	}
	id, err := createHousehold(db, h)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Huishouden aangemaakt: %s (ID %d)", h.Label, id)
	w.Header().Set("HX-Redirect", "/households/"+strconv.FormatInt(id, 10)+"/edit")
	w.WriteHeader(http.StatusOK)
}

func handleHouseholdUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h := householdFromForm(r)
	h.ID = id
	if h.Label == "" {
		http.Error(w, "aanhef/label is verplicht", http.StatusBadRequest)
		return
	}
	if err := updateHousehold(db, h); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Huishouden bijgewerkt: %s (ID %d)", h.Label, h.ID)
	w.Header().Set("HX-Redirect", householdsRedirectURL(h.ID))
	w.WriteHeader(http.StatusOK)
}

// householdsRedirectURL is contactsRedirectURL's twin for /households (see
// handlers.go) -- scrollTo=<id> only, the filter itself lives in
// sessionStorage client-side (see household_list.html). Used only after an
// update (handleHouseholdCreate keeps redirecting to the new household's own
// edit page, not the list, since adding members right away is the normal
// next step there).
func householdsRedirectURL(id int64) string {
	return "/households?scrollTo=" + strconv.FormatInt(id, 10)
}

func handleHouseholdDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	n, err := countHouseholdMembers(db, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n > 0 {
		http.Error(w, "dit huishouden heeft nog leden; verplaats hen eerst naar een ander huishouden", http.StatusBadRequest)
		return
	}
	if err := deleteHousehold(db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Huishouden verwijderd (ID %d)", id)
	// Empty 200 response: htmx removes the row via hx-swap="outerHTML".
	w.WriteHeader(http.StatusOK)
}

func householdFromForm(r *http.Request) Household {
	return Household{
		Label:   strings.TrimSpace(r.FormValue("label")),
		Address: strings.TrimSpace(r.FormValue("address")),
		Zip:     strings.TrimSpace(r.FormValue("zip")),
		City:    strings.TrimSpace(r.FormValue("city")),
		Country: strings.TrimSpace(r.FormValue("country")),
		Phone:   strings.TrimSpace(r.FormValue("phone")),
		Email:   strings.TrimSpace(r.FormValue("email")),
	}
}
