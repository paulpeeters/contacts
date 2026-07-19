# Contacts (Go + Htmx + SQLite)

A contacts manager built on Go for the backend, Htmx for interactivity (no
separate frontend build), and SQLite for storage. Uses the pure-Go
`modernc.org/sqlite` driver, so no C compiler is required.

## 1. Install prerequisites (Windows)

**Go compiler**
1. Download the installer from https://go.dev/dl/ (pick the Windows `.msi`).
2. Run it, accept the defaults. This adds `go` to your PATH.
3. Open a new terminal (Command Prompt or PowerShell) and confirm:
   ```
   go version
   ```
   should print something like `go version go1.23... windows/amd64`.

**DB Browser for SQLite** (optional but handy — lets you open `contacts.db`
and view/edit rows directly)
1. Download from https://sqlitebrowser.org/dl/ (Windows installer).
2. Install and open it any time to inspect `contacts.db` in this folder.
   Close DB Browser (or reload) before/after the app writes to it if you
   see a "file changed" prompt.

## 2. Set up the project

Open a terminal in this project's folder and run:

```
go mod tidy
```

This downloads `modernc.org/sqlite`, `github.com/xuri/excelize/v2` (Excel
export/sync), `github.com/go-pdf/fpdf` (label PDF generation) and
`github.com/gogpu/systray` (tray icon) and writes `go.sum`. You need internet
access for this one-time step; after that everything runs offline.

## 3. Run it

```
go run .
```

This opens your default browser automatically at http://localhost:8080.
Stop the server with Ctrl+C.

## 4. Build & deploy

`build\build.cmd` is the single entry point for building — it asks for a
target (`windows`, `linux` or `docker`) and builds accordingly. It never
hardcodes where the output should go: that's read from `build\build.env`
(gitignored — copy `build\build.env.template` to `build\build.env` first
and fill in your own local paths; without it, the build still runs, the
output just stays in the project folder instead of being copied anywhere).

```
build\build.cmd
```

**"linux" vs "docker" — these answer different questions, not "which OS":**
"linux" builds the plain binary that gets *deployed* (shipped to a real
server and run there — see "Deploying to a server" below; that server
happens to run it inside a Docker container too, via a generic runtime
image, but building the binary itself has nothing Docker-specific about
it). "docker" is for *local* testing on this machine via Docker Desktop
(`docker compose up`) and optionally pushing an image to Docker Hub — it
never touches the remote server at all. In short: need to ship a new
version to the server → `linux`. Want to test in a container on your own
PC, or publish an image → `docker`.

**Target: windows** — `go build -ldflags "-H=windowsgui -s -w" -o contacts.exe .`
(`-H=windowsgui`: no console window pops up, just the browser; `-s -w`:
strips debug info). Copies `contacts.exe` to `WINDOWS_DEPLOY_DIR` if set in
`build.env`.

**Target: linux** — `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ...`,
producing a static `contacts` binary (no C compiler needed, `modernc.org/sqlite`
is pure Go). Copies it to `LINUX_DEPLOY_DIR` if set — that's the folder a
separately maintained deploy script picks up and ships to the server (see
"Deploying to a server" below).

**Target: docker** — builds the local `contacts:latest` image
(`docker build -t contacts:latest .`, using the `Dockerfile` at the repo
root) for testing with `docker compose up` (see below), then asks whether
to also push it to Docker Hub. If yes, and `DOCKERHUB_IMAGE` is set in
`build.env` (e.g. `yourusername/contacts`), it tags and pushes
`DOCKERHUB_IMAGE:latest` — run `docker login` first if you haven't. This
target doesn't produce or copy anything a deploy script would use; it's
purely for local testing and/or publishing an image.

**Icon** (optional, one-time, Windows target only): `icon.ico` is already
in this folder; Go itself can't embed a Windows exe icon, so a small,
pure-Go helper tool writes it into a `.syso` resource file that `go build`
then links in automatically:

```
go install github.com/akavel/rsrc@latest
rsrc -ico icon.ico -o rsrc_windows_amd64.syso
```

Do this once (needs internet, to fetch the tool); re-run just the `rsrc`
line if you ever replace `icon.ico` with a different image. Keep the
resulting `rsrc_windows_amd64.syso` in this folder (checked into version
control) so future builds keep the icon without repeating this step. The
`_windows_amd64` suffix isn't cosmetic — Go only links a
`*_windows_amd64.syso` file into windows/amd64 builds, so it's
automatically skipped by the linux/docker targets instead of breaking
cross-compilation.

**Running the Windows build**: double-click `contacts.exe` (or run it from
a terminal). It opens your browser to http://localhost:8080 automatically.
Running it again while it's already open just re-opens the browser instead
of failing to start. Because everything (HTML templates included, via
`go:embed`) is baked into the single `.exe`, and the SQLite driver is pure
Go (no C compiler, no extra DLLs needed at runtime), deploying to another
Windows PC is just: copy `contacts.exe` (and `data/`, if you want to bring
existing data) to any folder on the other PC, then double-click it. No Go
installation, no `templates/` folder, no installer needed on the target
machine.

## 5. Deploy with Docker (local build + run)

The same codebase also builds and runs as a Linux container — no code
changes needed between the two, since `main.go` detects the OS at runtime
(`runtime.GOOS`) and adjusts behavior accordingly (no tray icon, no
auto-opened browser, binds to `0.0.0.0` instead of `127.0.0.1` by default,
listens for `SIGTERM`/`SIGINT` for a graceful `docker stop`).

```
docker compose up -d --build
```

This builds the image from the included `Dockerfile` (multi-stage: compiles
a static Linux binary with `CGO_ENABLED=0`, then copies just that binary
into a `scratch` runtime image — no Go, no shell, no package manager in the
final image) and starts it per `docker-compose.yml`: port `8080` on the
host mapped to `8080` in the container, and `./data` on the host
bind-mounted to `/app/data` in the container — the exact same folder the
Windows exe uses, so a `data/` folder copied from one deployment to the
other just works.

Without compose:

```
docker build -t contacts:latest .
docker run -d --name contacts -p 8080:8080 -v ./data:/app/data --restart unless-stopped contacts:latest
```

**Configuration**, both via environment variables (see `docker-compose.yml`
to set these permanently) and `data/appsettings.json` (works identically to
the Windows exe — the `"port"` field, see "Poort wijzigen" above):
- `CONTACTS_LISTEN_HOST` — overrides the listen address inside the
  container (default `0.0.0.0`, i.e. reachable from outside the container).
  Only worth setting to something else if you're putting a reverse proxy on
  the same Docker network and want to restrict it.
- `TZ` — defaults to `Europe/Brussels` in the provided `Dockerfile`
  (`scratch` has no timezone database of its own, so this matters for
  correct local timestamps in `contacts.log` and backup filenames). Override
  if you're deploying somewhere else.

**Not applicable in the container** (Windows-only, silently skipped): the
systray icon, auto-opening a browser, the "already running, just re-open
the browser" double-launch check, and the nav menu's "Afsluiten" button
(with `/shutdown` itself also rejecting the request server-side, in case
something posts to it directly) — none of these make sense for a headless
container (a self-inflicted shutdown would just have `restart: always`
relaunch it immediately), so `main.go` skips them there and just runs as a
plain foreground server until it receives a stop signal (`SIGTERM`/`SIGINT`).

### Deploying to a server (e.g. via Dockhand)

Some production setups don't build/push a custom image at all — instead a
generic, unmodified runtime image gets the actual application bind-mounted
in from the host and started via an explicit `command:` (the same pattern
many `dotnet publish`-based stacks use: a stock runtime image + published
app folder). `docker-compose.prod.yml` is that style of stack definition
for Contacts — kept here purely as a version-controlled **reference copy**;
Dockhand itself isn't pointed at this file, its stack config is entered by
pasting this YAML into the Dockhand UI. Update both together when they need
to change (e.g. a new port or image tag):

```yaml
name: contacts-prod
services:
  contacts:
    image: gcr.io/distroless/static-debian12
    container_name: contacts-prod
    restart: always
    ports:
      - 8080:8080
    working_dir: /app
    volumes:
      - /path/to/contacts-prod/app:/app
    command: ["/app/contacts"]
    environment:
      - TZ=Europe/Brussels
```

(Placeholders — fill in your own host port/path; see `docker-compose.prod.yml`
for the same content with more detail in the comments.)

One flat host folder holds the binary. `contacts.db`/`appsettings.json`/
`contacts.log` end up in `app/data/` — the app's own `dataDir`, always
resolved relative to its own binary (see `main.go`). That subfolder must be
protected from being overwritten at the **deploy-script** level (not via
Docker) — whatever ships the binary to the server needs a rule like rsync's
`--filter=P /data/***` so its `--delete` never touches `app/data/`,
otherwise a deploy would wipe the database.

The Go equivalent of ".NET runtime image + published dotnet output" is "a
base image with just tzdata/ca-certificates + a statically-linked Go
binary" — `gcr.io/distroless/static-debian12` fits that role (~1.9MB,
includes tzdata and ca-certificates, no shell/package manager needed since
`CGO_ENABLED=0` gives the binary zero runtime dependencies of its own).

**Building** the deployable artifact: `build\build.cmd` (target: `linux` —
*not* `docker`, see "Build & deploy" above for why) stages the linux/amd64
binary at whatever local folder `build.env`'s `LINUX_DEPLOY_DIR` points to.

**Deploying**: intentionally *not* part of this repository — which server,
which paths, SSH targets and so on are infrastructure details, not
application source. A deploy script (rsync-based, or whatever fits your
infrastructure) should: read the binary from `LINUX_DEPLOY_DIR`, protect
the remote `app/data/` folder from deletion/overwrite as described above,
ship the binary, mark it executable, restart the container, and ideally
keep a backup of the previous version. Pick whatever host port is free on
the target host for the left-hand side of `ports:`; the container-side
port must stay `8080`. No `CONTACTS_LISTEN_HOST` override is needed —
`main.go` already defaults to `0.0.0.0` on Linux.

The `Dockerfile`/`docker-compose.yml`/`.dockerignore` at the repo root are
for **local** build-and-run only (`docker compose up -d --build`, see
above) — not used by this production path.

## What it does

- **Startpagina** (`/home`, "Home" in het menu): een korte uitleg per
  onderdeel van de app, bedoeld als eerste aanknopingspunt — de gebruiker
  kiest daarna zelf een onderdeel uit het menu.
- **Appversie**: het versienummer (bv. "1.0.0") staat als `AppVersion`-constante
  bovenaan `version.go` en wordt getoond naast "Contactenbeheer" in het menu,
  op de startpagina, en in de tooltip van het systray-icoontje. Er is geen
  automatisch ophoog-proces — pas de constante met de hand aan en bouw
  opnieuw wanneer je een nieuwe versie wil uitbrengen.
- **Databaseversie**: naast de appversie houdt `version.go` ook een
  `CurrentSchemaVersion`-constante bij, apart opgeslagen in de database zelf
  (tabel `schema_meta`, zie `ensureSchemaVersion` in `db.go`). Dit is een
  extra veiligheidsnet bovenop de bestaande, altijd-uitgevoerde
  `CREATE TABLE IF NOT EXISTS`/`ALTER TABLE`-migraties: een database die
  geopend wordt door een **oudere** app dan waarmee ze laatst gebruikt werd,
  wordt geweigerd met een duidelijke foutmelding (in plaats van stilzwijgend
  data te missen of te corrumperen); een database die **ouder** is dan wat
  de huidige app verwacht, wordt automatisch stap voor stap opgehoogd (nu nog
  geen enkele stap nodig, `CurrentSchemaVersion` staat op 1). Verhoog deze
  constante en voeg een `case` toe in `ensureSchemaVersion`'s `switch`
  wanneer een toekomstige wijziging aan `db.go` bestaande databases echt moet
  migreren.
- **Datamap (`./data`)**: `appsettings.json`, het standaard `contacts.db` en
  `contacts.log` leven allemaal in een `data`-submap naast `contacts.exe`
  (`exeDir/data`, aangemaakt bij opstart als hij nog niet bestaat) — dezelfde
  map die bij een Docker-deploy als volume gemount wordt (zie "Deploy with
  Docker" verderop), zodat de exe en de container zich identiek gedragen.
  Stond je vorige installatie nog met deze bestanden rechtstreeks naast
  `contacts.exe` (van vóór deze wijziging)? Bij de eerste opstart na de
  update verplaatst `migrateLegacyDataFiles` (`migrate.go`) ze automatisch
  naar `data/` — stil, eenmalig, en enkel als ze nog op die oude
  standaardlocatie stonden. Een database die je zelf via de kiezer naar een
  ander pad (bv. een netwerkschijf) hebt gestuurd, wordt hierbij nooit
  aangeraakt.
- **Database kiezen** (modal op de startpagina, knop "Database wisselen"): bij
  het opstarten wordt automatisch het laatst gebruikte databasepad geopend,
  onthouden in `appsettings.json` (in `data/`, veld
  `last_db_path`) — standaard `data/contacts.db` als er nog geen
  instellingenbestand is. De eerste keer dat `/home` laadt in een sessie
  verschijnt automatisch een modal met dat pad, met drie knoppen: **"Wisselen"**
  opent een bestaand databasebestand op het ingevoerde pad; **"Nieuwe database
  aanmaken"** maakt op dat pad een volledig lege, nieuwe database aan en
  **weigert** met een duidelijke foutmelding als daar al een bestand bestaat,
  zodat je nooit per ongeluk een bestaande database heropent terwijl je dacht
  dat je met een lege begon; **"Doorgaan met huidige"** sluit de modal gewoon
  zonder iets te wijzigen. Beide eerste acties sluiten bij succes de huidige
  databaseverbinding, openen de nieuwe (en weigeren/migreren op basis van
  diens schemaversie, zie hierboven), en onthouden het nieuwe pad voor de
  volgende keer. Een echte bestandsverkenner is vanuit een webpagina niet
  mogelijk (een `<input type="file">` geeft om veiligheidsredenen nooit een
  absoluut pad terug) — het pad moet dus met de
  hand getypt/geplakt worden.
  - **UNC-paden** (bv. `\\nas-1\data\contacts.db`) worden ondersteund.
    `openDB` (`db.go`) geeft het pad rechtstreeks door aan de SQLite-driver
    zonder er een `?...`-query-string aan toe te voegen — dat laatste zou de
    driver ertoe brengen het pad als een URI te behandelen, en SQLite's
    URI-schrijfwijze voor UNC-paden vraagt een specifieke, makkelijk
    verkeerd te doen escaping (5 slashes: `file://///server/share/db`). Een
    gewoon pad (zonder `file:`-prefix) gaat rechtstreeks door SQLite's
    normale bestandsafhandeling, die UNC-paden al lang gewoon ondersteunt.
    De twee PRAGMA's die vroeger via die query-string liepen
    (`foreign_keys`, `busy_timeout`) worden nu na het openen als gewone SQL
    uitgevoerd in plaats van in de DSN.
  - **Als een verkeerd/onbereikbaar pad in `appsettings.json` blijft staan**
    (bv. een NAS die offline is bij de volgende opstart) faalt het opstarten
    zelf met een Windows-foutvenstertje, nog vóór de webserver (en dus de
    database-kiezer) beschikbaar is. Los je dat op door `appsettings.json`
    (in `data/`) te verwijderen of het `last_db_path`-veld erin leeg te
    maken — de app valt dan terug op het standaardpad (`data/contacts.db`)
    en je kan de netwerklocatie via de modal opnieuw proberen zodra ze weer
    bereikbaar is.
- **Poort wijzigen**: Contacts luistert standaard op poort 8080
  (`http://127.0.0.1:8080`). Is die op een bepaalde pc al in gebruik door
  iets anders, voeg dan een `"port"`-veld toe aan `appsettings.json` (in
  `data/` — maak het bestand aan als het nog niet bestaat) en herstart
  Contacts:
  ```json
  { "port": 9090 }
  ```
  Er is voorlopig geen schermpje hiervoor in de app zelf — dit veld met de
  hand toevoegen/aanpassen en herstarten volstaat. Ontbreekt `"port"` (of
  staat het op `0`), dan wordt gewoon 8080 gebruikt zoals voorheen. Het
  gekozen poortnummer wordt ook door de app zelf teruggeschreven naar
  `appsettings.json` (samen met `last_db_path`) zodat het bewaard blijft bij
  latere acties zoals database wisselen.
  - `appsettings.json` zelf staat in `.gitignore` (het is per-installatie,
    kan een lokaal databasepad/poort bevatten die niet bij iedereen past) —
    `appsettings.json.template` staat wel in de repo als voorbeeld/startpunt;
    kopieer het naar `data/appsettings.json` en pas het aan als je met een
    kant-en-klaar startpunt wil beginnen in plaats van de standaardwaarden.
    De app werkt trouwens ook prima zonder dat je dit bestand ooit aanmaakt:
    ontbreekt het, dan gebruikt Contacts gewoon de standaardwaarden
    (`data/contacts.db`, poort 8080) en maakt het bestand vanzelf aan zodra
    je iets wisselt.
- **Back-up maken** (knop naast "Database wisselen" op de startpagina, POST
  `/backup-db`, `backup.go`): maakt meteen een kopie van de actieve database
  naast het origineel, met dezelfde naam plus een tijdstip erin, bv.
  `contacts.db` &rarr; `contacts-20260717-153012.db`. Gebruikt SQLite's eigen
  `VACUUM INTO` in plaats van de ruwe bestandsbytes te kopiëren — dat neemt
  een consistente momentopname via SQLite zelf, dus een schrijfactie die
  net op dat moment bezig is kan nooit een kapotte kopie opleveren. Er is
  geen automatische opruiming van back-ups (in tegenstelling tot de
  logarchieven) — dat blijft voorlopig manueel.
- Lists all contacts, add/edit/delete via forms and Htmx (delete removes
  the row in place, no page reload). Every confirmation (delete, afsluiten,
  exporteren) and error message goes through a shared styled modal dialog
  instead of the browser's native `alert()`/`confirm()` popups — see
  `appAlert`/`appConfirm` in `nav.html`.
- Leaving the contact or household edit form via a link elsewhere in the
  page (e.g. "Huishoudgegevens bewerken" or "+ Nieuw contact in dit
  huishouden") warns first, via that same modal, if the form has unsaved
  changes — comparing serialized form state on click, so nothing is lost
  by an unintended navigation. Opening "+ Nieuw contact in dit huishouden"
  also pre-selects that household in the new contact's picker.
- The contact form's Tags field is still plain comma-separated free text,
  but the "Kies uit bestaande tags" button next to it opens a checkbox
  picker (in the same shared modal style) listing every distinct tag any
  contact already uses, pre-checked to match the current field. Applying it
  overwrites the field with the checked tags plus any custom ones you'd
  already typed that aren't in the list yet, so nothing typed is lost.
- **Contacts vs. households**: a contact is one person (first/last name,
  gender, birthdate, personal mobile, personal e-mail, tags, "last verified
  on" date). The shared mailing address, shared/landline phone, shared
  e-mail, and the free-text card salutation ("Familie Peeters-Janssens",
  "Piet en Greet Peeters-Janssens en kinderen", ...) live on a separate
  **household**
  instead, since this app's main purpose is printing one label per
  household, not one per person. Every contact belongs to exactly one
  household; a household can have any number of contacts (a couple, a
  family, or just one person).
  - Manage households on their own page (**Huishoudens** in the menu):
    add/edit the shared address/phone/email/label, see its current
    members, and delete a household once it has no members left.
  - On the contact form, pick an existing household from a dropdown or
    create a new one inline. Editing an *existing* household's shared data
    is deliberately only possible from the Huishoudens page — not from the
    contact form — to avoid silently overwriting another household's data
    if you switch the dropdown without reloading the page.
- **Filteren op /contacts en /households**: a client-side filter panel
  ("Filteren") on both list pages — first name/last name/e-mail/address/
  city (substring, per field) plus a birth-year comparison (`<`, `=`, `>`)
  and a tag filter (per tag: neutral / must-have / must-not-have, combined
  with an EN/OF toggle). On /households, the personal-field conditions
  (name/e-mail/birth year/tags) match if *any* member of that household
  satisfies them; address/city match the household itself. The active
  filter is kept in the browser's `sessionStorage` (cleared when the
  browser/tab session ends), so it survives opening a contact/household and
  coming back — via Opslaan, Annuleren, or the nav menu — no matter how you
  got back to the list; "Filter wissen" resets it. The list also
  auto-scrolls back to the row you just viewed or saved.
- Data lives in `contacts.db`, a SQLite file created automatically the
  first time you run the app. Open it with DB Browser for SQLite any time.
- **Upgrading an existing `contacts.db` from before households existed**:
  on first startup after this update, the app automatically adds the new
  `household_id` column and creates one household per existing contact
  (seeded from that contact's old address/phone/email columns, with the
  card label defaulting to "Firstname Lastname"). Nothing is deleted — the
  old address/phone/email columns stay in the table, just unused going
  forward. Afterwards, use the Huishoudens page to merge people who
  actually share an address into one household.
  - If your database predates households, its old `email` column (back
    then the contact's only email address) is automatically reused as the
    new **personal** email field — nothing is lost, you'll just see that
    old value show up in the "E-mail (persoonlijk)" field on the contact
    form rather than sitting unused. Databases created after the household
    split but before this field existed get a fresh, empty `email` column
    added instead.
- **Exporteren / Synchroniseren naar/vanuit Excel** (knoppen op de
  contactenlijst) — an ID-based round-trip feature for bulk-correcting data
  (fix addresses, fill in missing fields, ...) directly in Excel and syncing
  the result back. There used to be a second, separate "Importeren vanuit
  Excel" feature with fuzzy column-header matching for onboarding an
  arbitrary external spreadsheet; it was removed as redundant once sync
  existed, and `/contacts/sync` gained a sample-file download instead (see
  below) so a new user still has something to start from.
  - **Exporteren** writes one `.xlsx` row per contact with every personal
    field plus its household's shared fields flattened in (fixed column
    names: `ContactID`, `HouseholdID`, `FirstName`, `LastName`, `Gender`,
    `Birthdate`, `Mobile`, `PersonalEmail`, `Tags`, `LastVerifiedOn`,
    `HouseholdLabel`, `Address`, `Zip`, `City`, `Country`, `Phone`, `Email`).
    A household with several members is duplicated across their rows on
    purpose — that's the tradeoff of a flat, one-row-per-person sheet.
  - **Synchroniseren** reads that same fixed column set back (by exact
    header name, not fuzzy-matched) and, per row: `ContactID`/`HouseholdID`
    of `0` (or blank) creates a new contact or household; a filled-in,
    existing ID **fully overwrites** that record with the row's values —
    including blanking out a field you cleared in Excel (this is a full
    overwrite, not a "patch only what's filled in" merge). A row whose data
    is byte-for-byte identical to what's already in the database is left
    alone entirely (no write, no `updated_at` bump) and shown as
    "ongewijzigd" rather than "bijwerken" on the preview screen — only rows
    that actually change something count towards "bijgewerkt".
  - `Birthdate` and `LastVerifiedOn` are exported with an explicit Text
    (`@`) number format on their whole column, so Excel keeps whatever ISO
    `yyyy-mm-dd` text you see exactly as typed even after you edit or add a
    date in that column — without this, Excel silently reinterprets a
    date-shaped cell using your regional short-date setting (e.g.
    `dd/mm/yyyy`) the moment you touch it, which risked day/month ambiguity
    on the next sync.
  - Sync only ever creates or updates. Removing a row from the sheet before
    syncing back does **not** delete anything — deleting a contact or
    household is still only done via their respective list pages.
  - Two (or more) rows both wanting a brand-new *shared* household (both
    `HouseholdID` = 0) are grouped automatically when their household fields
    (label/address/zip/city/country/phone/email) match byte-for-byte — one
    household gets created for the group, and every row in it points at it.
    If even one of those fields differs (a typo, a different phone format,
    ...) the rows are treated as wanting separate households instead, so
    check the preview screen's "nieuw (groep N)" labels before confirming —
    rows sharing the same group number will share one household.
  - Before anything is written, a preview screen shows counts (new/updated
    contacts and households) and a row-by-row table, including which
    "groep" number each new-household row belongs to. Referencing an
    unknown `ContactID`/`HouseholdID`, or reusing the same `ContactID`
    twice, blocks confirmation until the file is fixed. The same
    `HouseholdID` appearing with different shared-field values on multiple
    rows doesn't block anything, but is flagged as a warning ("last row
    wins") so you notice if that was unintentional.
  - **Voorbeeld-Excel downloaden** (link op `/contacts/sync`): a ready-made
    `.xlsx` with the expected header row plus two sample contacts sharing
    one household (`ContactID`/`HouseholdID` both `0`, with identical
    household fields), for a new user who doesn't have an export to start
    from yet. Since both sample rows have matching household data, syncing
    the sample as-is creates one shared household for both, demonstrating
    the grouping behavior above.
- **Etiketten printen** (menu bovenaan: **Etikettenbladen**, **Etiketinhoud**,
  **Afdrukfilters**) is opgesplitst in drie losse, onafhankelijke delen:
  1. **Etikettenblad** (`/label-sheets`): de fysieke kant van een vel
     etiketten — papierformaat (A4/Letter/A5-snelkeuze of aangepast), aantal
     etiketten horizontaal/verticaal, randafstanden en tussenruimte, alles in
     mm. Etiketgrootte wordt automatisch berekend, met een live rasterdiagram
     (SVG) om de getallen meteen visueel te checken. Stel je meestal maar
     éénmalig in per soort vel dat je in huis hebt; meerdere bladen kunnen
     naast elkaar bestaan.
  2. **Etiketinhoud** (`/label-contents`): welk veld waar komt op één etiket
     — een lijst van elementen (regels), elk op een eigen x/y-positie in mm
     met een eigen font (Helvetica/Arial, Times New Roman of Courier),
     grootte, vet en cursief, plus een marge binnen het etiket (enkel een
     ontwerp-hulplijn, niet afgedwongen bij het afdrukken). Een element is
     vrije tekst waarin je velden combineert via `{FieldName}`-plaatshouders,
     bv. `{FirstName} {LastName}` of `Tel: {Phone}` — zo zet je meerdere
     velden en vaste tekst naast elkaar op één regel vóór je de positie
     bepaalt. De knop "+ veld" voegt de plaatshouder in op de
     cursorpositie. Naast persoonlijke velden (voornaam, achternaam,
     `{Mobile}`, `{PersonalEmail}`, ...) kan je ook huishoudvelden gebruiken
     — `{HouseholdLabel}` (de aanhef, bv. "Familie Peeters-Janssens"),
     `{Address}`, `{Zip}`, `{City}`, `{Country}`, `{Phone}`, `{Email}` — die
     van het huishouden van het contact komen, niet van het contact zelf.
     Let op het verschil tussen `{Mobile}`/`{PersonalEmail}`/`{ID}`
     (persoonlijk) en `{Phone}`/`{Email}`/`{HouseholdID}` (gedeeld,
     huishouden). `{SeqNo}` is de 1-gebaseerde volgorde van dit
     contact/huishouden binnen de huidige afdruk — zie "Checklist" verderop.
     Een inhoud is **niet gekoppeld aan één specifiek etikettenblad** — dezelfde
     inhoud kan met eender welk blad gecombineerd worden zolang het etiket
     groot genoeg is (dat wordt enkel losjes gecontroleerd, zie hieronder).
     De **live weergave** van één etiket (CSS, met een vast voorbeeldcontact
     "Jan Janssens") toont een snelle inschatting van layout en tekstlengte
     terwijl je ontwerpt, tegen een zelf gekozen etikettenblad (enkel om de
     juiste afmeting te tonen, niet opgeslagen) — geen pixel-perfecte proef,
     dat is de PDF-proefdruk. Die proefdruk staat onderaan de bewerkpagina
     van een opgeslagen inhoud: kies een etikettenblad en krijg een PDF
     gevuld met het voorbeeldcontact en lichte hulplijnen rond elk etiket,
     om tegen een leeg of gedeeltelijk gebruikt vel te houden — inclusief een
     instelbare startpositie.
  3. **Afdrukfilter** (`/label-filters`): een bewaarbare selectie van
     contacten/huishoudens (zoekterm, tags, afdrukmodus) plus de keuze welk
     etikettenblad en welke inhoud ermee afgedrukt worden — hetzelfde scherm
     dient zowel om een filter te bewaren voor later ("Filter bewaren") als
     om meteen te printen; bewaren is optioneel, afdrukken werkt ook met een
     nieuw, niet-bewaard filter. Op dit scherm: een zoekveld en tag-filters
     (client-side, direct) om de contactenlijst te versmallen, "alles
     selecteren/deselecteren (zichtbaar)" om in bulk te werken, en losse
     checkboxes per contact voor individuele uit-/insluiting bovenop een
     filter. Een bewaard filter onthoudt enkel de *criteria* (zoekterm, tags,
     modus, blad, inhoud) — niet de aangevinkte contacten zelf: bij het
     heropenen worden de contacten die aan de criteria voldoen automatisch
     opnieuw aangevinkt, maar blijven daarna vrij aan te passen. Bovenaan
     het scherm kies je ook "Contact" (standaard, één etiket per aangevinkt
     contact) of "Huishouden" (één etiket per huishouden, ook als je meerdere
     leden ervan aanvinkt — checken van 1 lid volstaat). Bij "Huishouden"
     blijven persoonlijke velden (`{FirstName}`, `{Mobile}`,
     `{PersonalEmail}`, `{ID}`, ...) leeg op het etiket, omdat er dan geen
     "dé" contactpersoon meer is; gebruik in dat geval huishoudvelden zoals
     `{HouseholdLabel}`, `{Address}` en `{HouseholdID}`. De volgorde van de
     afgedrukte huishoudens is alfabetisch op aanhef. Een losse waarschuwing
     verschijnt als de gekozen inhoud er, op basis van de posities van haar
     elementen, niet lijkt te passen op het gekozen blad — een indicatie, geen
     garantie; controleer altijd met de proefdruk.
  - **Postale opmaak**: `{City}` wordt altijd in hoofdletters afgedrukt
    (postale conventie). `{ForeignCountry}` doet hetzelfde voor het land,
    maar blijft leeg zodra het land van het huishouden overeenkomt met het
    ingestelde "eigen land" (of leeg is) — zo verschijnt het land enkel op
    etiketten voor het buitenland, en niet op binnenlandse post. Het eigen
    land is instelbaar op de **Instellingen**-pagina (standaard "België");
    de vergelijking is hoofdletter- en accentongevoelig. `{Country}` zelf
    blijft ongewijzigd (geen hoofdletters, altijd getoond) voor wie dat
    letterlijk wil gebruiken.
  - **Checklist (PDF, A4 liggend)**: op het selectiescherm staan twee losse
    knoppen naast elkaar, beide werkend op dezelfde selectie/afdrukmodus
    hierboven: "Etiketten genereren (PDF)" en "Checklist genereren (PDF, A4
    liggend)". De checklist is een tabel om met de hand in te vullen: Volgnr,
    ID, Naam, Geschreven, Gestuurd, Ontvangen, Opmerkingen — altijd A4
    liggend, ongeacht de papiergrootte/oriëntatie van het etikettensjabloon
    zelf. Volgnr en ID/Naam volgen dezelfde volgorde en dezelfde
    contact-vs-huishouden-keuze als de etiketten (ID = HouseholdID en Naam =
    de aanhef bij "Huishouden", anders het persoonlijke ID en voornaam +
    achternaam). Zet `{SeqNo}` als veld op je etikettensjabloon om hetzelfde
    volgnummer ook op het etiket zelf te tonen — zo kan je een etiket en
    zijn checklist-rij altijd aan elkaar koppelen, ook als de stapel
    etiketten losraakt van de lijst.
    Bewust **twee losse knoppen/PDF's** in plaats van één klik die alles
    ineens genereert: (1) `go-pdf/fpdf` (zoals de hele gofpdf-familie
    waarvan het is afgeleid) heeft een gekende beperking waarbij liggende en
    staande pagina's mixen binnen één PDF-document alles laat terugvallen op
    de oriëntatie van de eerste pagina, dus de checklist moet een eigen
    document zijn; (2) twee tabbladen tegelijk laten openen vanuit één klik
    (bv. via verborgen formulieren of `window.open`) wordt door
    pop-upblokkering vaak maar voor de helft toegelaten — met twee aparte
    knoppen is elke tab het resultaat van zijn eigen, echte gebruikersklik en
    wordt dus nooit geblokkeerd.
  - Gebruikt de pure-Go PDF-bibliotheek `go-pdf/fpdf`, dus geen extra
    installatie nodig. **Belangrijk bij afdrukken**: kies "Werkelijke
    grootte / 100%", niet "passend op pagina" — anders klopt de uitlijning
    niet meer. Alle tekst op etiketten én checklist gaat door
    `UnicodeTranslatorFromDescriptor("")` (cp1252/WinAnsi) vlak voor het
    getekend wordt — de ingebouwde kernfonts (Helvetica/Times/Courier)
    verwachten die encoding, niet ruwe UTF-8; zonder deze vertaling
    verschijnen accenten als mojibake (bv. "Italië" wordt "ITALIÃ‹").
- **Windows-app**: `contacts.exe` opent automatisch je standaardbrowser bij
  het opstarten (naar http://localhost:8080), zonder consolevenster. Alle
  HTML-templates zitten ingebakken in de exe (`go:embed`), dus voor gebruik
  op een andere pc volstaat het kopiëren van `contacts.exe` (+ `contacts.db`
  als je bestaande data wil meenemen) — geen Go-installatie, geen
  `templates/`-map, geen installer nodig. Start je de exe een tweede keer
  terwijl hij al draait, dan opent gewoon opnieuw de browser in plaats van
  een foutmelding te tonen. Een **Logs**-pagina (in het menu bovenaan) toont
  de laatste serverberichten, want zonder consolevenster is dat anders
  onzichtbaar. Als het opstarten zelf faalt (bv. database niet te openen),
  verschijnt een Windows-foutvenstertje — dat gaat namelijk aan de
  webserver (en dus de Logs-pagina) vooraf.
  - **Waar leven de logs?** Op twee plekken tegelijk. `logRing` (`logs.go`)
    is een in-memory ringbuffer die enkel de laatste 500 regels bijhoudt —
    dat vult de in-app **Logs**-pagina. Daarnaast schrijft `filelog.go` elke
    log-regel ook naar **`contacts.log`** in `data/` (niet in `contacts.db`
    — dat blijft puur contactgegevens). Beide krijgen exact dezelfde regels,
    via één `log.SetOutput(io.MultiWriter(...))` in `main.go`.
  - **Bugfix**: beide bleven aanvankelijk helemaal leeg, ook al werkte de rest
    van de app prima. Oorzaak: `io.MultiWriter` stopt bij de eerste writer
    die een fout teruggeeft en slaat alle volgende writers in de lijst dan
    over. Op een `-H=windowsgui`-build (geen console) kan een schrijfpoging
    naar `os.Stdout` mislukken — en omdat stdout de eerste writer in de lijst
    was, werd daardoor *elke* logregel stilzwijgend tegengehouden vóór ze
    `logRing` of `contacts.log` ooit bereikte. Opgelost met `safeWriter` in
    `main.go`, een kleine wrapper rond `os.Stdout` die schrijffouten altijd
    negeert (en dus nooit de rest van de keten blokkeert) — `logRing` en het
    logbestand zelf falen in de praktijk nooit, dus die hoefden niet gewrapt
    te worden.
  - **Rotatie**: bij elke opstart wordt een `contacts.log` van een vorige
    sessie eerst hernoemd naar `contacts-<tijdstip-laatste-schrijving>.log`
    (bv. `contacts-20260717-143210.log`), waarna een lege `contacts.log`
    voor de nieuwe sessie begint. Lukt die hernoeming niet (zeldzaam, bv.
    schrijfrechten), dan wordt gewoon verder geschreven in het bestaande
    bestand — er gaat nooit iets verloren.
  - **Opruiming**: ook bij elke opstart worden archiefbestanden
    (`contacts-*.log`) die meer dan 1 maand oud zijn (op basis van hun
    laatste-wijzigingsdatum) automatisch verwijderd — zie `cleanupOldLogs`
    in `filelog.go`. De actieve `contacts.log` zelf wordt hierbij nooit
    aangeraakt, enkel de gearchiveerde bestanden van eerdere sessies.
    Lukt het wegschrijven naar `contacts.log` niet (bv. map niet
    beschrijfbaar), dan blijft de app gewoon werken via stdout en de
    Logs-pagina — enkel de bestandslogging valt dan weg.
  - **Wat wordt er gelogd**: bij opstart o.a. de appversie, programmamap,
    databasepad (en of dat van `appsettings.json` komt of het standaardpad
    is), schemaversie-check, geladen templates, luisteradres, automatisch
    geopende browser en systray-status; tijdens gebruik o.a. aanmaken/
    bijwerken/verwijderen van contacten en huishoudens, database
    wisselen/aanmaken, eigen land wijzigen, gegenereerde etiketten-/
    checklist-PDF's (aantal + gebruikte inhoud/blad), Excel-export
    (aantal rijen) en een samenvatting bij elke bevestigde sync
    (nieuw/bijgewerkt/ongewijzigd per contacten en huishoudens).
- **Icoontje in het systeemvak (systray)**: `contacts.exe` toont een icoontje
  in het systeemvak (rechts onderaan naast de klok) zolang hij draait, met
  tooltip "Contacts" bij het hoveren. Klikken op het icoontje opent
  meteen de browser; rechtsklikken toont een menu met "Openen in browser" en
  "Afsluiten" (sluit de app netjes af, inclusief de database). Dit is naast
  de "Afsluiten"-knop in het browsermenu een tweede, snellere manier om de
  app te stoppen zonder Taakbeheer. Gebruikt de pure-Go library
  `gogpu/systray` (geen C-compiler nodig, past bij de rest van dit project),
  maar die library is zelf pas sinds mei 2026 uit en dus nog weinig beproefd
  — als de tray onverwacht niet zou werken, blijft de app zelf via de
  browser gewoon bruikbaar (zie "Known limitations").
  - Het systray-icoontje (`icon.png`, ingebakken via `go:embed`, 256x256) is
    een eenvoudig, zelf getekend ontwerp: een teal kaartje met een
    contactpersoon-silhouet en tekstregels, als "adresboek"-symbool. Vervang
    het gewoon door je eigen `icon.png` (vierkant, transparante achtergrond)
    in de projectmap en bouw opnieuw — geen codewijziging nodig.
  - **Het icoontje van `contacts.exe` zelf** (zichtbaar in Verkenner,
    snelkoppelingen, "Openen met", ...) is een *apart* ding van het
    systray-icoontje hierboven — Windows haalt dat uit een ingebakken
    resource in de exe, niet uit `icon.png`. `icon.ico` (meerdere formaten:
    16 tot 256px, zelfde ontwerp als `icon.png`) staat al in de projectmap;
    zie "Build a standalone Windows app" hierboven voor de (eenmalige)
    `rsrc`-stap om het effectief in `contacts.exe` in te bakken. Sla je die
    stap over, dan werkt de app identiek, alleen met het standaard generieke
    Go-executable-icoontje.

## Project layout

```
main.go              entry point: embed, dataDir resolution, routing,
                     listen host + SIGTERM/SIGINT handling, startup error
                     handling; Windows-only (isDesktop): browser auto-open,
                     already-running check, systray icon + message loop
icon.png              systray-icoon (placeholder, PNG, ingebakken via go:embed)
version.go              AppVersion + CurrentSchemaVersion constanten (hier
                       met de hand aanpassen om te versiebeheren)
appsettings.go          appsettings.json laden/opslaan (last_db_path, port)
migrate.go              eenmalige migratie van root-level contacts.db/
                       appsettings.json naar data/ (oude standaardlocatie)
dbchooser.go            POST /choose-db handler (database live wisselen)
backup.go               POST /backup-db handler (back-up via VACUUM INTO)
db.go                SQLite connection, schema, migration, all queries,
                       ensureSchemaVersion (databaseversie-check/upgrade)
handlers.go           HTTP handlers (list/add/edit/delete)
households.go          Household handlers (list/add/edit/delete)
home.go                 Startpagina (/home), uitleg + database-chooser-modal-status
sync.go                Excel export + ID-based sync (export, sample download,
                       preview, confirm) + shared helpers (targetField,
                       normalizeDate, normalizeHeader, householdLabelOrDefault)
settings.go             Instellingen (eigen land voor {ForeignCountry})
sheets.go              Etikettenblad-CRUD (papier/raster/marges/gap) + gedeelde
                       form-parsing helpers (strAt/parseFloatField/parseIntField)
contents.go             Etiketinhoud-CRUD (velden/posities/font), proefdruk-route,
                       labelFieldOptions/fontOptions
filters.go              Afdrukfilter-CRUD + opbouw van het gecombineerde
                       selectie+print-scherm (labelFilterFormDataFor)
pdf.go                 PDF-opbouw met go-pdf/fpdf: raster, placeholders, tekst,
                       accent-vertaling (cp1252), losse checklist-PDF (A4 liggend)
print.go                Batch-PDF generatie (/label-prints, /label-prints/checklist)
                       + labelContentFitsSheet (server-side fit-check) +
                       sanitizeFilename/dedupeByHousehold/collectDistinctTags
logs.go                Ringbuffer voor log-regels + /logs handler
filelog.go              contacts.log wegschrijven, rotatie bij opstart,
                       opruiming van archieven ouder dan 1 maand
msgbox_windows.go       Windows-foutvenster (MessageBoxW) bij opstartfouten
msgbox_other.go         Fallback voor niet-Windows (console-log)
models.go             Contact / Household / ContactListRow /
                       formData / contactsListData / LabelSheet / LabelContent /
                       LabelFilter / LabelElement / LabelContact /
                       labelSheetFormData / labelContentFormData /
                       labelFilterFormData / labelFilterListData structs
templates/
  nav.html            gedeeld navigatiemenu (Home/Contacten/Huishoudens/
                      Etikettenbladen/Etiketinhoud/Afdrukfilters/Instellingen/Logs)
                      + gedeelde modal (appAlert/appConfirm, vervangt alert()/
                      confirm(), incl. onderschepping van htmx:confirm)
  home.html           startpagina: korte uitleg per menu-onderdeel + database-
                      chooser-modal (auto-open eenmaal per opstart)
  logs.html           logs-pagina
  contacts_list.html  contact list page (+ filterpaneel + sync summary banner)
  contact_form.html    add/edit page + household picker
  household_list.html  overzicht huishoudens + leden
  household_form.html  add/edit huishouden (adres/telefoon/e-mail/aanhef)
  sync_upload.html      export/sync uitleg + voorbeeld-Excel downloaden + upload form
  sync_preview.html     counts + rij-per-rij overzicht + bevestigen (of blokkerende fouten)
  label_sheet_list.html   overzicht opgeslagen etikettenbladen
  label_sheet_form.html   papier/raster/marge-instellingen + live SVG-rasterpreview
  label_content_list.html overzicht opgeslagen etiketinhoud
  label_content_form.html elementenlijst + live CSS-preview (tegen een gekozen
                          blad) + proefdruk-sectie (kleine inline JS)
  label_filter_list.html  overzicht opgeslagen afdrukfilters
  label_filter_form.html  filterinstellingen + contactselectie (zoeken + tri-state
                          tag-filter, zelfde EN/OF-mechanisme als /contacts en
                          /households) + twee print-knoppen + "Filter bewaren"
                          (client-side JS, geen server round-trip voor de selectie)
  settings.html         instellingen (eigen land)
```

All templates are embedded into the binary at build time via `go:embed`
(`main.go`) — the `templates/` folder only needs to exist at build time,
not alongside the running exe.

Docker-related files (not needed for the Windows exe build, only for
`docker compose up`, see "Deploy with Docker" above):

```
Dockerfile              multi-stage build: static Linux binary -> scratch image
.dockerignore           keeps data/, .git, build artifacts out of the build context
docker-compose.yml      local build + run (port mapping + ./data volume mount)
docker-compose.prod.yml reference copy of the Dockhand stack config (pasted
                       into the Dockhand UI, not read from this file
                       automatically) -- see "Deploying to a server" above
rsrc_windows_amd64.syso Windows-only exe icon resource -- the _windows_amd64
                       suffix means Go only links it into windows/amd64
                       builds, so the Docker/Linux build ignores it
```

Build tooling (see "Build & deploy" above):

```
build/
  build.cmd              single entry point for building -- prompts for a
                        target (windows/linux/docker), builds accordingly
  build.env.template      documents the expected build.env format, committed
  build.env               your own local build-output paths -- gitignored,
                        never committed (copy build.env.template to create it)
```

Deploy scripts (rsync/backup/restart to a remote server, or whatever fits
your infrastructure) are deliberately **not** part of this repository --
they contain environment-specific details (hostnames, remote paths) that
don't belong in application source. See "Deploying to a server" above for
what such a script needs to do.

## Known limitations

- Validation is minimal (only first/last name are required, enforced by
  the browser's built-in HTML validation plus a basic server-side check).
- Single DB connection (`SetMaxOpenConns(1)`) — fine for one user testing
  locally, not meant for concurrent multi-user load.
- No auth — anyone who can reach port 8080 can use the app.
- Uploaded sync files are parsed in memory and held server-side (keyed by
  a random id in a hidden form field) between the preview and confirm
  steps; nothing is written to disk, and pending syncs older than an
  hour are dropped. Fine for one local user, not a general upload service.
- Label element rows are added/removed client-side with a small vanilla
  JS snippet (clone a `<template>` row / remove a row). Needed because a
  drag-and-drop visual label designer would be a much bigger, more fragile
  piece of work than this project's Go+Htmx+SQLite scope calls for. This
  sits alongside a handful of other small, page-local JS snippets (the
  shared alert/confirm modal in nav.html, live label previews, unsaved-changes
  warnings on the contact/household forms) — still no framework, no build
  step, just vanilla JS plus Htmx.
- The grid/size preview on the label form is a schematic SVG (rectangles
  only, no text), meant to sanity-check your margin/gap numbers. The
  one-label content preview uses CSS mm units and a fixed sample contact —
  useful to check layout and text length, but **not** a proof of what will
  actually print. That's what the PDF proof print is for.
- The contact selection page loads every contact into the page at once and
  filters with JS (no pagination, no server-side search). Fine at
  contact-book scale; would need rethinking for a very large list.
- "Start bij etiket" on the real print counts from the first label of the
  first page only (it doesn't currently split unevenly across multiple
  sheets in a special way) — with more contacts than fit on one page, later
  pages simply start filling from position 1 again, which is what you'd
  want for consecutive full sheets after an offset first sheet.
- Etiketten zaten oorspronkelijk in één gecombineerd "sjabloon" (papier +
  raster + marges + inhoud in één). Bij eerste opstart na de opsplitsing
  migreert `migrateLabelTemplatesSplit` (db.go) elk bestaand sjabloon
  automatisch naar een Etikettenblad + Etiketinhoud + Afdrukfilter met
  dezelfde naam — dit gebeurt eenmalig en stil, er is niets manueel nodig.
  De oude `label_templates`-tabel blijft ongebruikt in de database staan
  (nooit gedropt).
- PDF text does not wrap or auto-shrink: each element is drawn as a single
  line starting at its X/Y position. If the text is longer than fits on the
  label, it simply runs past the edge instead of wrapping to a second line
  — keep an eye on this in the proof print, especially for combined fields
  like `{FirstName} {LastName}` with long names.
- An unrecognized `{Placeholder}` (typo in a field name) is left as literal
  text in the output rather than silently becoming blank, so mistakes are
  easy to spot in the proof print.
- The export/sync feature never deletes anything (by design, confirmed with
  the user), and doesn't (yet) support merging two brand-new contacts into
  one brand-new shared household in a single pass (see the sync upload page).
- `{ForeignCountry}`'s home-country match is a single setting, applied the
  same way to every household — there's no per-household override for an
  edge case like a Belgian household living permanently abroad. The
  on-page live label preview shows a fixed example value for
  `{ForeignCountry}` (to make the placeholder visible while designing);
  the real PDF proof print uses the actual sample household's country
  ("België") against the real setting, so it correctly renders blank
  there unless you've changed the home country in Instellingen — that's
  intentional, not a bug, but can look inconsistent at first glance.
- On Windows, the app binds only to `127.0.0.1` (localhost), not your
  network IP — by design, so it's not reachable from other devices on your
  network. In the Docker deployment it binds to `0.0.0.0` by default instead
  (see "Deploy with Docker"), since a container is meaningless if nothing
  outside it can reach the service; both are overridable via the
  `CONTACTS_LISTEN_HOST` environment variable. Either way, think about the
  lack of authentication before exposing this beyond your own machine.
- The "already running?" check (a plain HTTP GET to `/contacts` on
  localhost, good enough to avoid a confusing port-already-in-use error when
  you double-click the exe twice) — along with the systray icon and
  auto-opening a browser — only runs on Windows (`runtime.GOOS == "windows"`
  in `main.go`). None of the three make sense for a headless Docker
  container, which just runs as a plain foreground server instead.
- With `-H=windowsgui`, a crash or fatal error *before* the web server
  starts (e.g. the database file can't be created) shows a native Windows
  message box, since there's no console and no Logs page yet at that
  point. Anything that goes wrong *after* the server is up shows up on the
  Logs page instead.
- Households list N+1 queries (one query for the household, one per
  household for its members) — fine at contact-book scale, not something
  you'd want at real scale.
- Deleting a household is blocked while it still has members (you get an
  error asking you to move them first) rather than cascading the delete —
  a deliberate safety choice so you can't lose a shared address by mistake.
- No syncing to an external contact list (Google, Outlook, ...) yet. The
  household split was done partly with this in mind (each contact can be
  addressed independently with its own household's address), but the
  actual sync integration is not built.
- The systray icon (`gogpu/systray`) is a very new library (first release
  May 2026) — it's pure Go (fits this project's "no C compiler" approach)
  but far less battle-tested than the mature alternatives, which all
  require CGO + a C compiler to build. If the tray icon fails to appear or
  behaves oddly, the app keeps working normally through the browser and the
  "Afsluiten" button there; only the tray is affected.
- The systray message loop must run on the same goroutine that calls
  `main()` (a Windows/OS requirement for pumping UI messages) — the HTTP
  server runs in a background goroutine instead. If you extend `main.go`,
  keep `runTray()` as the last, blocking call in `main()`.
- Killing the process abruptly (Task Manager "End task" instead of the
  "Afsluiten" option) skips the tray icon's own cleanup; Windows usually
  removes it right away, but occasionally leaves a "ghost" icon in the tray
  until you hover over that spot — a general Windows quirk, not specific to
  this app.
- The database-chooser modal takes a typed/pasted path, not a real file
  picker — see "Database kiezen" above. "Wisselen" opens whatever's at that
  path, creating an empty database there if nothing exists yet (same
  behavior as the very first run) — worth double-checking if you actually
  expected an existing file to be found. "Nieuwe database aanmaken" is the
  explicit, safer alternative: it refuses outright if a file already exists
  at the given path, so it can't be used to accidentally re-open (and think
  you're starting fresh with) an existing database.
- `appsettings.json` and `schema_meta` (in the database) are new as of the
  app-version/database-version feature — an older `contacts.db` created
  before this feature has no `schema_meta` row yet, so it's silently
  stamped with `CurrentSchemaVersion` the first time it's opened by a build
  that has this feature, without any explicit migration step.
- I could not run an actual Go compiler in my sandbox to verify any of
  this builds (no internet access to fetch the toolchain there), so
  please run `go mod tidy && go build -ldflags "-H=windowsgui -s -w" -o contacts.exe .`
  locally as the first check. Let me know if you hit any build errors and
  I'll fix them.
