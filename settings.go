package main

import (
	"log"
	"net/http"
	"strings"
)

func handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	hc, err := getHomeCountry(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	render(w, "settings", settingsData{HomeCountry: hc})
}

func handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hc := strings.TrimSpace(r.FormValue("home_country"))
	if hc == "" {
		http.Error(w, "eigen land mag niet leeg zijn", http.StatusBadRequest)
		return
	}
	if err := updateHomeCountry(db, hc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Eigen land bijgewerkt naar %q", hc)
	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusOK)
}
