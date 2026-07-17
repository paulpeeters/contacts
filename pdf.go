package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
)

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

// contactFieldValue returns the string value of a named field on a label
// contact -- either a personal Contact field or a joined household field.
// Field names match those used in the label element placeholders (e.g.
// "FirstName", "ID", "HouseholdLabel"). homeCountry is the configured "own
// country" (Instellingen page) used by the {ForeignCountry} placeholder.
func contactFieldValue(c LabelContact, field, homeCountry string, seq int) string {
	switch field {
	case "SeqNo":
		return strconv.Itoa(seq)
	case "ID":
		if c.ID == 0 {
			return "" // household-mode label: no single contact to identify
		}
		return strconv.FormatInt(c.ID, 10)
	case "HouseholdID":
		return strconv.FormatInt(c.HouseholdID, 10)
	case "FirstName":
		return c.FirstName
	case "LastName":
		return c.LastName
	case "HouseholdLabel":
		return c.HouseholdLabel
	case "Address":
		return c.Address
	case "Zip":
		return c.Zip
	case "City":
		// Postal convention: city in capitals on a mailed label.
		return strings.ToUpper(c.City)
	case "Country":
		return c.Country
	case "ForeignCountry":
		// Blank for an empty country or one matching the configured home
		// country (postal convention: don't print the country on local
		// mail); otherwise the country name in capitals, same as City.
		country := strings.TrimSpace(c.Country)
		if country == "" || normalizeHeader(country) == normalizeHeader(homeCountry) {
			return ""
		}
		return strings.ToUpper(country)
	case "Email":
		return c.Email // household's shared email
	case "PersonalEmail":
		return c.Contact.Email // the contact's own email
	case "Phone":
		return c.Phone
	case "Mobile":
		return c.Mobile
	case "Gender":
		return c.Gender
	case "Birthdate":
		return c.Birthdate
	case "Tags":
		return c.Tags
	case "LastVerifiedOn":
		return c.LastVerifiedOn
	default:
		return ""
	}
}

func knownLabelField(field string) bool {
	for _, f := range labelFieldOptions {
		if f.Value == field {
			return true
		}
	}
	return false
}

// renderElementContent substitutes {FieldName} placeholders in a label
// element's Content with values from the given contact. An unrecognized
// placeholder (e.g. a typo) is left untouched in the output rather than
// silently turned into an empty string, so mistakes stay visible.
func renderElementContent(content string, c LabelContact, homeCountry string, seq int) string {
	return placeholderRe.ReplaceAllStringFunc(content, func(m string) string {
		field := m[1 : len(m)-1]
		if knownLabelField(field) {
			return contactFieldValue(c, field, homeCountry, seq)
		}
		return m
	})
}

func fpdfFontStyle(bold, italic bool) string {
	s := ""
	if bold {
		s += "B"
	}
	if italic {
		s += "I"
	}
	return s
}

// sampleLabelContact is fixed placeholder data used for proof prints. It
// matches the sample shown in the on-page label preview so what you see
// while designing lines up with what the PDF proof shows.
func sampleLabelContact() LabelContact {
	return LabelContact{
		Contact: Contact{
			ID:             42,
			HouseholdID:    7,
			FirstName:      "Jan",
			LastName:       "Janssens",
			Mobile:         "0475 12 34 56",
			Email:          "jan.persoonlijk@example.com",
			Gender:         "M",
			Birthdate:      "1980-05-14",
			Tags:           "vriend, werk",
			LastVerifiedOn: "2026-01-10",
		},
		HouseholdLabel: "Familie Janssens-Peeters",
		Address:        "Kerkstraat 12",
		Zip:            "9000",
		City:           "Gent",
		Country:        "België",
		Phone:          "09 123 45 67",
		Email:          "jan.janssens@example.com",
	}
}

// buildLabelsPDF renders one line of content per label element for each
// given contact onto a grid of labels described by sheet (paper size,
// columns/rows, margins, gap), with the placeholders in content's elements
// filled in, starting at the given 0-based label position (row-major: left
// to right, top to bottom) on the first page, wrapping onto additional
// pages as needed. If showBorders is true a thin rectangle is drawn around
// each label cell -- useful for proof prints held up against a blank or
// partially-used sheet to check alignment; real batch prints should turn
// this off. sheet and content are deliberately independent (see
// LabelContent's doc comment) -- any sheet can be combined with any
// content, it's just up to the person printing to pick a sensible pairing.
func buildLabelsPDF(sheet LabelSheet, content LabelContent, contacts []LabelContact, startIndex int, showBorders bool) (*fpdf.Fpdf, error) {
	if sheet.Cols <= 0 || sheet.Rows <= 0 {
		return nil, fmt.Errorf("ongeldig raster: aantal etiketten horizontaal/verticaal moet groter dan 0 zijn")
	}
	lw := sheet.LabelWidthMM()
	lh := sheet.LabelHeightMM()
	if lw <= 0 || lh <= 0 {
		return nil, fmt.Errorf("ongeldige etiketgrootte: controleer papierformaat, randen en tussenruimte")
	}

	homeCountry, err := getHomeCountry(db)
	if err != nil {
		return nil, err
	}

	perPage := sheet.Cols * sheet.Rows
	if startIndex < 0 {
		startIndex = 0
	}
	startIndex = startIndex % perPage

	// fpdf (like the whole gofpdf family it's forked from) expects the Size
	// passed here in "natural" shorter-side-first order and does its own
	// swap to match OrientationStr -- passing an already-landscape-shaped
	// Size (Wd > Ht) together with OrientationStr "L" does NOT skip that
	// swap, it un-swaps it back to portrait. So always pass (shorter,
	// longer) and let orientationStr do the flipping.
	orientation := "P"
	paperMin, paperMax := sheet.PaperWidthMM, sheet.PaperHeightMM
	if sheet.PaperWidthMM > sheet.PaperHeightMM {
		orientation = "L"
		paperMin, paperMax = sheet.PaperHeightMM, sheet.PaperWidthMM
	}
	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: orientation,
		UnitStr:        "mm",
		Size:           fpdf.SizeType{Wd: paperMin, Ht: paperMax},
	})
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)

	// Core fonts (Helvetica/Times/Courier) expect single-byte cp1252
	// (WinAnsi) encoded text, not raw UTF-8 -- without this translation,
	// accented characters (é, ë, ï, ...) come out as mojibake (e.g. "Italië"
	// becomes "ITALIÃ‹") because the UTF-8 bytes get reinterpreted one at a
	// time as cp1252 code points.
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	slot := startIndex
	currentPage := -1

	for i, c := range contacts {
		seq := i + 1 // 1-based position in this print batch, independent of
		// the physical starting slot on the sheet -- this is what {SeqNo}
		// prints and what lines up with the Volgnr column on the checklist
		// built by buildChecklistPDF, since both iterate the same contacts
		// slice in the same order.
		pageNum := slot / perPage
		pageSlot := slot % perPage

		if pageNum != currentPage {
			pdf.AddPage()
			currentPage = pageNum
		}

		col := pageSlot % sheet.Cols
		row := pageSlot / sheet.Cols
		x0 := sheet.MarginLeftMM + float64(col)*(lw+sheet.GapHMM)
		y0 := sheet.MarginTopMM + float64(row)*(lh+sheet.GapVMM)

		if showBorders {
			pdf.SetDrawColor(180, 180, 180)
			pdf.Rect(x0, y0, lw, lh, "D")
		}

		for _, el := range content.Elements {
			text := renderElementContent(el.Content, c, homeCountry, seq)
			if strings.TrimSpace(text) == "" {
				continue
			}
			family := el.FontFamily
			if family == "" {
				family = "Helvetica"
			}
			size := el.FontSize
			if size <= 0 {
				size = 10
			}
			pdf.SetFont(family, fpdfFontStyle(el.Bold, el.Italic), size)
			pdf.Text(x0+el.X, y0+el.Y, tr(text))
		}

		slot++
	}

	return pdf, nil
}

// checklistColumn describes one column of the checklist table built by
// buildChecklistPDF: a header label, a width in mm, and a cell alignment.
type checklistColumn struct {
	Header string
	Width  float64
	Align  string
}

// checklistColumns is the fixed column layout for the checklist, chosen to
// fit an A4 landscape page (297mm wide, 10mm margins each side -> 277mm of
// usable width; these widths sum to 277).
var checklistColumns = []checklistColumn{
	{"Volgnr", 15, "C"},
	{"ID", 15, "C"},
	{"Naam", 80, "L"},
	{"Geschreven", 30, "C"},
	{"Gestuurd", 30, "C"},
	{"Ontvangen", 30, "C"},
	{"Opmerkingen", 77, "L"},
}

// buildChecklistPDF renders a standalone, always-landscape A4 PDF with a
// hand-fillable checklist table: one row per contact, in the exact same
// order as the labels built by buildLabelsPDF for the same selection
// (buildLabelsPDF's seq/{SeqNo} numbering is simply the 1-based position in
// that same contacts slice), so the Volgnr column here always matches the
// {SeqNo} printed on the corresponding label. householdMode picks whether
// the ID/Naam columns show the household or the personal identity -- pass
// the same value used to decide whether contacts was deduped by household
// before the labels were built, so the two stay consistent.
//
// This is deliberately its own separate PDF document rather than pages
// appended onto the labels PDF: go-pdf/fpdf (like the gofpdf family it's
// forked from) has a known limitation where mixing portrait and landscape
// pages in a single document collapses everything to the first page's
// orientation (see https://github.com/jung-kurt/gofpdf/issues/317) -- since
// label sheets are very often portrait, appending forced-landscape pages
// onto that same document silently turned them portrait too. Building a
// second document that is landscape from its very first page sidesteps
// that bug entirely; print.go opens it as a second tab from the same click.
func buildChecklistPDF(contacts []LabelContact, householdMode bool, title string) *fpdf.Fpdf {
	const (
		pageW    = 297.0
		pageH    = 210.0
		marginMM = 10.0
		rowH     = 8.0
		titleH   = 10.0
	)
	usableWidth := pageW - 2*marginMM

	// Wd/Ht passed here must be in natural, shorter-side-first order (see
	// the matching comment in buildLabelsPDF) -- pageH (210, the shorter
	// side) goes in Wd, pageW (297) in Ht, and OrientationStr "L" swaps
	// them to the real, final landscape 297x210.
	pdf := fpdf.NewCustom(&fpdf.InitType{
		OrientationStr: "L",
		UnitStr:        "mm",
		Size:           fpdf.SizeType{Wd: pageH, Ht: pageW},
	})
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetMargins(0, 0, 0)

	tr := pdf.UnicodeTranslatorFromDescriptor("")

	drawHeaderRow := func() {
		pdf.SetFont("Helvetica", "B", 9)
		pdf.SetFillColor(230, 230, 230)
		pdf.SetXY(marginMM, pdf.GetY())
		for _, col := range checklistColumns {
			pdf.CellFormat(col.Width, rowH, tr(col.Header), "1", 0, "C", true, 0, "")
		}
		pdf.Ln(rowH)
	}

	firstPage := true
	newPage := func() {
		pdf.AddPage()
		pdf.SetXY(marginMM, marginMM)
		if firstPage {
			pdf.SetFont("Helvetica", "B", 13)
			pdf.CellFormat(usableWidth, titleH, tr(title), "", 0, "L", false, 0, "")
			pdf.Ln(titleH)
			pdf.SetX(marginMM)
			firstPage = false
		}
		drawHeaderRow()
	}

	newPage()
	bottomLimit := pageH - marginMM

	for i, c := range contacts {
		if pdf.GetY()+rowH > bottomLimit {
			newPage()
		}

		id := c.HouseholdID
		name := c.HouseholdLabel
		if !householdMode {
			id = c.ID
			name = strings.TrimSpace(c.FirstName + " " + c.LastName)
		}

		values := []string{
			strconv.Itoa(i + 1),
			strconv.FormatInt(id, 10),
			tr(name),
			"", "", "", "", // Geschreven, Gestuurd, Ontvangen, Opmerkingen -- blank boxes to fill by hand
		}

		pdf.SetFont("Helvetica", "", 9)
		pdf.SetX(marginMM)
		for ci, col := range checklistColumns {
			pdf.CellFormat(col.Width, rowH, values[ci], "1", 0, col.Align, false, 0, "")
		}
		pdf.Ln(rowH)
	}

	return pdf
}
