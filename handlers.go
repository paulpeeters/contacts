package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func handleIndex(w http.ResponseWriter, r *http.Request) {
	contacts, err := listContactsWithHousehold(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, err := listDistinctContactTags(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := contactsListData{Contacts: contacts, AllTags: tags}
	q := r.URL.Query()
	if q.Has("contacts_created") || q.Has("contacts_updated") || q.Has("households_created") || q.Has("households_updated") {
		data.ShowSyncSummary = true
		data.ContactsCreated, _ = strconv.Atoi(q.Get("contacts_created"))
		data.ContactsUpdated, _ = strconv.Atoi(q.Get("contacts_updated"))
		data.HouseholdsCreated, _ = strconv.Atoi(q.Get("households_created"))
		data.HouseholdsUpdated, _ = strconv.Atoi(q.Get("households_updated"))
	}
	render(w, "contacts_list", data)
}

// handleNewForm renders the blank "add contact" form. If a household_id
// query param is present (used by the "+ Nieuw contact in dit huishouden"
// link on the household edit page), that household is pre-selected in the
// picker instead of defaulting to "create a new household".
func handleNewForm(w http.ResponseWriter, r *http.Request) {
	households, err := listHouseholds(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, err := listDistinctContactTags(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := formData{Households: households, AllTags: tags}
	if hidStr := r.URL.Query().Get("household_id"); hidStr != "" {
		if hid, perr := strconv.ParseInt(hidStr, 10, 64); perr == nil {
			data.Contact.HouseholdID = hid
		}
	}
	render(w, "contact_form", data)
}

func handleEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := getContact(db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	households, err := listHouseholds(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tags, err := listDistinctContactTags(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "contact_form", formData{Contact: *c, Households: households, AllTags: tags})
}

func handleCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c := contactFromForm(r)
	if c.FirstName == "" || c.LastName == "" {
		http.Error(w, "first and last name are required", http.StatusBadRequest)
		return
	}
	hid, err := resolveHouseholdID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.HouseholdID = hid
	id, err := createContact(db, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Contact aangemaakt: %s %s (ID %d)", c.FirstName, c.LastName, id)
	w.Header().Set("HX-Redirect", contactsRedirectURL(id))
	w.WriteHeader(http.StatusOK)
}

func handleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c := contactFromForm(r)
	c.ID = id
	if c.FirstName == "" || c.LastName == "" {
		http.Error(w, "first and last name are required", http.StatusBadRequest)
		return
	}
	hid, err := resolveHouseholdID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.HouseholdID = hid
	if err := updateContact(db, c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Contact bijgewerkt: %s %s (ID %d)", c.FirstName, c.LastName, c.ID)
	w.Header().Set("HX-Redirect", contactsRedirectURL(c.ID))
	w.WriteHeader(http.StatusOK)
}

// contactsRedirectURL builds the /contacts redirect target used after
// create/update: scrollTo=<id> so the list auto-scrolls to the contact that
// was just saved. The list's own filter isn't threaded through here -- it
// lives in sessionStorage on the client (see contacts_list.html) and gets
// re-applied on every load regardless of how /contacts was reached.
func contactsRedirectURL(id int64) string {
	return "/contacts?scrollTo=" + strconv.FormatInt(id, 10)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := deleteContact(db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Contact verwijderd (ID %d)", id)
	// Empty 200 response: htmx removes the row via hx-swap="outerHTML".
	w.WriteHeader(http.StatusOK)
}

func contactFromForm(r *http.Request) Contact {
	return Contact{
		FirstName:      strings.TrimSpace(r.FormValue("first_name")),
		LastName:       strings.TrimSpace(r.FormValue("last_name")),
		Mobile:         r.FormValue("mobile"),
		Email:          strings.TrimSpace(r.FormValue("email")),
		Gender:         r.FormValue("gender"),
		Birthdate:      r.FormValue("birthdate"),
		Tags:           strings.TrimSpace(r.FormValue("tags")),
		LastVerifiedOn: r.FormValue("last_verified_on"),
	}
}

// resolveHouseholdID figures out which household a submitted contact form
// belongs to. The household picker either submits an existing household's
// id, or the literal value "new" together with a set of inline
// new-household fields (reusing householdFromForm) -- in which case a new
// household is created on the spot and its id is used. Editing an
// *existing* household's shared fields deliberately isn't possible from
// this form: that goes through the dedicated household edit page instead,
// to avoid silently overwriting another household's data if the dropdown
// is switched without a page reload.
func resolveHouseholdID(r *http.Request) (int64, error) {
	v := strings.TrimSpace(r.FormValue("household_id"))
	if v == "" || v == "new" {
		h := householdFromForm(r)
		if h.Label == "" {
			return 0, fmt.Errorf("nieuw huishouden vereist een aanhef/label")
		}
		return createHousehold(db, h)
	}
	return strconv.ParseInt(v, 10, 64)
}
