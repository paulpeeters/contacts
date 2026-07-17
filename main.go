package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/gogpu/systray"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed icon.png
var iconPNG []byte

var (
	db      *sql.DB
	tmpl    *template.Template
	logRing = &ringBuffer{max: 500}
	appTray *systray.SystemTray

	// exeDir is the directory the running executable lives in, computed once
	// at startup. currentDBPath is whichever database file is currently
	// open, and dbChooserShown tracks whether the database-chooser modal has
	// already auto-opened once this run (see handleHome in home.go). logFile
	// is contacts.log for this run (see filelog.go) -- nil if file logging
	// couldn't be set up, in which case logging still works via stdout and
	// the in-app /logs page, just not persisted to disk. listenAddr/appURL
	// used to be consts (always port 8080) -- they're now resolved early in
	// main() from appsettings.json's "port" field, falling back to
	// defaultPort, since another program on some PCs already occupies 8080.
	exeDir         string
	currentDBPath  string
	currentPort    int
	dbChooserShown bool
	logFile        *os.File
	listenAddr     string
	appURL         string
)

const (
	defaultPort = 8080
	appName     = "Contacts"
)

// safeWriter wraps an io.Writer and never reports a write error, so it can
// be used as one entry in an io.MultiWriter without a failure there
// aborting delivery to the writers listed after it. See its use on
// os.Stdout in main().
type safeWriter struct{ w io.Writer }

func (s safeWriter) Write(p []byte) (int, error) {
	s.w.Write(p) // error deliberately ignored -- see doc comment above
	return len(p), nil
}

func main() {
	// safeStdout wraps os.Stdout so a failed write there can never break
	// logging: io.MultiWriter aborts and skips every remaining writer the
	// moment ANY writer in its list returns an error. A -H=windowsgui build
	// has no attached console, and os.Stdout.Write can fail there (e.g.
	// "invalid handle") -- without this wrapper, that single failure would
	// silently swallow every log line before it ever reached logRing (the
	// in-app /logs page) or contacts.log, which is exactly why both stayed
	// empty. logRing and contacts.log writes essentially never fail on
	// their own, so wrapping just stdout here is enough.
	safeStdout := safeWriter{os.Stdout}
	log.SetOutput(io.MultiWriter(safeStdout, logRing))
	log.Printf("Contacts v%s wordt opgestart.", AppVersion)

	// exeDir and appsettings.json both have to be read before the
	// "already running?" check below, since that check needs to know which
	// port to probe -- appsettings.json's "port" field (see appSettings in
	// appsettings.go) can override the default, e.g. when another program
	// on this PC already occupies 8080. Everything else that reads
	// `settings` (the database path) still happens further down, unchanged.
	var err error
	exeDir, err = executableDir()
	if err != nil {
		fail("Contacts - fout bij opstarten", fmt.Sprintf("Kon de locatie van het programma niet bepalen:\n%v", err))
	}

	settings := loadAppSettings(exeDir)
	currentPort = settings.Port
	portSource := "ingesteld via appsettings.json"
	if currentPort == 0 {
		currentPort = defaultPort
		portSource = "standaardpoort"
	}
	listenAddr = fmt.Sprintf("127.0.0.1:%d", currentPort)
	appURL = fmt.Sprintf("http://127.0.0.1:%d", currentPort)

	if alreadyRunning(appURL + "/contacts") {
		log.Println("Contacts lijkt al te draaien op " + appURL + ", browser wordt geopend.")
		openBrowser(appURL)
		return
	}

	log.Printf("Programmamap: %s", exeDir)
	log.Printf("Poort: %d (%s)", currentPort, portSource)

	// contacts.log lives next to the exe. This is layered on top of, not
	// instead of, the existing stdout + in-app /logs ringbuffer -- if it
	// can't be set up (e.g. read-only folder), we just carry on without it.
	if lf, lfErr := setupFileLogging(exeDir); lfErr != nil {
		log.Printf("kon niet naar %s loggen (logs blijven wel beschikbaar op stdout en /logs): %v", logFileName, lfErr)
	} else {
		logFile = lf
		log.SetOutput(io.MultiWriter(safeStdout, logRing, logFile))
		log.Printf("Logbestand: %s (archieven ouder dan 1 maand worden bij opstart automatisch opgeruimd)", filepath.Join(exeDir, logFileName))
	}

	// The database path used to always be exeDir/contacts.db. Now the last
	// path the user chose (via the database-chooser modal on the home page)
	// is remembered in appsettings.json and reused on the next launch,
	// falling back to the original default if there's no settings file yet.
	currentDBPath = settings.LastDBPath
	dbPathSource := "laatst gebruikt pad (appsettings.json)"
	if currentDBPath == "" {
		currentDBPath = filepath.Join(exeDir, "contacts.db")
		dbPathSource = "standaardpad (geen appsettings.json gevonden)"
	}
	log.Printf("Databasepad: %s (%s)", currentDBPath, dbPathSource)

	db, err = openDB(currentDBPath)
	if err != nil {
		fail("Contacts - fout bij opstarten", fmt.Sprintf("Kon de database niet openen (%s):\n%v", currentDBPath, err))
	}
	// Written as a closure (not a plain "defer db.Close()") because the
	// database-chooser feature reassigns the global db at runtime -- a plain
	// defer would capture today's *sql.DB value immediately and close the
	// wrong (stale) connection at shutdown if the user has since switched
	// databases.
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	log.Printf("Database geopend, schema OK (versie %d).", CurrentSchemaVersion)

	if err := saveAppSettings(exeDir, appSettings{LastDBPath: currentDBPath, Port: currentPort}); err != nil {
		log.Printf("kon appsettings.json niet wegschrijven: %v", err)
	}

	tmpl, err = template.New("").Funcs(template.FuncMap{
		"AppVersion": func() string { return AppVersion },
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		fail("Contacts - fout bij opstarten", fmt.Sprintf("Kon de ingebouwde templates niet laden:\n%v", err))
	}
	log.Println("Templates geladen.")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/home", http.StatusFound)
	})
	mux.HandleFunc("GET /home", handleHome)
	mux.HandleFunc("POST /choose-db", handleChooseDBSubmit)
	mux.HandleFunc("POST /backup-db", handleBackupDB)
	mux.HandleFunc("GET /contacts", handleIndex)
	mux.HandleFunc("GET /contacts/new", handleNewForm)
	mux.HandleFunc("POST /contacts", handleCreate)
	mux.HandleFunc("GET /contacts/export", handleContactExport)
	mux.HandleFunc("GET /contacts/sync", handleSyncForm)
	mux.HandleFunc("GET /contacts/sync/sample", handleSyncSample)
	mux.HandleFunc("POST /contacts/sync/preview", handleSyncPreview)
	mux.HandleFunc("POST /contacts/sync/confirm", handleSyncConfirm)
	mux.HandleFunc("GET /contacts/{id}/edit", handleEditForm)
	mux.HandleFunc("PUT /contacts/{id}", handleUpdate)
	mux.HandleFunc("DELETE /contacts/{id}", handleDelete)

	mux.HandleFunc("GET /households", handleHouseholdList)
	mux.HandleFunc("GET /households/new", handleHouseholdNewForm)
	mux.HandleFunc("POST /households", handleHouseholdCreate)
	mux.HandleFunc("GET /households/{id}/edit", handleHouseholdEditForm)
	mux.HandleFunc("PUT /households/{id}", handleHouseholdUpdate)
	mux.HandleFunc("DELETE /households/{id}", handleHouseholdDelete)

	// Etiketten in 3 losse delen: het etikettenblad (papier/raster/marges),
	// de inhoud (welk veld waar op één etiket) en het afdrukfilter
	// (contactselectie + welk blad/inhoud). Zie de doc comments op
	// LabelSheet/LabelContent/LabelFilter in models.go.
	mux.HandleFunc("GET /label-sheets", handleLabelSheetList)
	mux.HandleFunc("GET /label-sheets/new", handleLabelSheetNewForm)
	mux.HandleFunc("POST /label-sheets", handleLabelSheetCreate)
	mux.HandleFunc("GET /label-sheets/{id}/edit", handleLabelSheetEditForm)
	mux.HandleFunc("PUT /label-sheets/{id}", handleLabelSheetUpdate)
	mux.HandleFunc("DELETE /label-sheets/{id}", handleLabelSheetDelete)

	mux.HandleFunc("GET /label-contents", handleLabelContentList)
	mux.HandleFunc("GET /label-contents/new", handleLabelContentNewForm)
	mux.HandleFunc("POST /label-contents", handleLabelContentCreate)
	mux.HandleFunc("GET /label-contents/{id}/edit", handleLabelContentEditForm)
	mux.HandleFunc("PUT /label-contents/{id}", handleLabelContentUpdate)
	mux.HandleFunc("DELETE /label-contents/{id}", handleLabelContentDelete)
	mux.HandleFunc("GET /label-contents/{id}/proof", handleLabelContentProof)

	mux.HandleFunc("GET /label-filters", handleLabelFilterList)
	mux.HandleFunc("GET /label-filters/new", handleLabelFilterNewForm)
	mux.HandleFunc("POST /label-filters", handleLabelFilterCreate)
	mux.HandleFunc("GET /label-filters/{id}/edit", handleLabelFilterEditForm)
	mux.HandleFunc("PUT /label-filters/{id}", handleLabelFilterUpdate)
	mux.HandleFunc("DELETE /label-filters/{id}", handleLabelFilterDelete)

	mux.HandleFunc("POST /label-prints", handleLabelPrintGenerate)
	mux.HandleFunc("POST /label-prints/checklist", handleLabelChecklistGenerate)

	mux.HandleFunc("GET /settings", handleSettingsForm)
	mux.HandleFunc("PUT /settings", handleSettingsUpdate)

	mux.HandleFunc("GET /logs", handleLogs)
	mux.HandleFunc("POST /shutdown", handleShutdown)

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fail("Contacts - fout bij opstarten", fmt.Sprintf("Kon niet starten op %s (misschien al in gebruik):\n%v", listenAddr, err))
	}

	// The HTTP server runs in the background; the main goroutine is reserved
	// for the systray message loop below, since Win32 (and other platform)
	// message loops need to be pumped from the thread that created the tray
	// icon.
	go func() {
		log.Printf("Contacts luistert op %s", appURL)
		if err := http.Serve(ln, mux); err != nil {
			log.Println("HTTP-server gestopt:", err)
		}
	}()

	// Open the browser only once we know the listener is actually up.
	go func() {
		time.Sleep(300 * time.Millisecond)
		openBrowser(appURL)
	}()

	runTray()
}

// runTray sets up the system tray icon (name/tooltip, "Openen in browser" and
// "Afsluiten" menu items, click-to-open) and pumps its message loop. This is
// the app's only visible presence when built with -H=windowsgui: there's no
// console and no program window, just this tray icon plus whatever browser
// tab the user has open.
func runTray() {
	menu := systray.NewMenu()
	menu.Add("Openen in browser", func() { openBrowser(appURL) })
	menu.AddSeparator()
	menu.Add("Afsluiten", func() {
		log.Println("Afgesloten via het systray-menu.")
		quitApp()
	})

	appTray = systray.New()
	appTray.SetIcon(iconPNG).
		SetTooltip(appName + " v" + AppVersion).
		SetMenu(menu)
	appTray.OnClick(func() { openBrowser(appURL) })
	appTray.Show()
	log.Println("Systray-icoon gestart.")

	if err := appTray.Run(); err != nil {
		log.Println("Kon systray-icoon niet starten (werkt de app verder gewoon door in de browser):", err)
		// Run() failed without blocking: fall back to blocking here so this
		// goroutine (and thus the process, since it's running on main)
		// doesn't exit and take the HTTP server goroutine down with it.
		select {}
	}
}

// quitApp closes the database and exits the process. Shared by the
// /shutdown HTTP handler and the systray "Afsluiten" menu item.
func quitApp() {
	log.Println("Database wordt gesloten, Contacts sluit af.")
	if appTray != nil {
		appTray.Remove()
	}
	if db != nil {
		db.Close()
	}
	if logFile != nil {
		logFile.Close()
	}
	os.Exit(0)
}

// fail logs a startup error, shows a native message box (a windowsgui
// build has no console, so without this a startup failure would be
// completely silent), and exits. Only used for errors that happen before
// the HTTP server -- and therefore the in-app Logs page -- exists.
func fail(title, message string) {
	log.Println(message)
	showFatalError(title, message)
	os.Exit(1)
}

// alreadyRunning does a quick HTTP GET to check whether a Contacts
// instance is already serving at the given URL, so launching the exe a
// second time (e.g. double-clicking it again) just opens the browser
// instead of failing to bind the port.
func alreadyRunning(url string) bool {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// openBrowser opens the given URL in the default Windows browser.
func openBrowser(url string) {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		log.Printf("kon browser niet automatisch openen (open %s handmatig): %v", url, err)
		return
	}
	log.Printf("Browser geopend: %s", url)
}

// executableDir returns the directory containing the running executable,
// so the database file lives next to contacts.exe regardless of the
// working directory the app happened to be launched from.
func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}

// handleShutdown lets the user stop the app from the browser -- handy if
// the tray icon is hard to find, or the tray failed to start. The response
// is flushed before the process exits, so the browser still gets to show a
// confirmation instead of a broken connection.
func handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<main class="container"><h1>Contacts afgesloten</h1><p>Dit tabblad/venster kan gesloten worden.</p></main>`)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		log.Println("Afgesloten op verzoek van de gebruiker (/shutdown).")
		quitApp()
	}()
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
