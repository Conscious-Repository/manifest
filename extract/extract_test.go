package extract

import (
	"archive/zip"
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// buildXLSX writes a minimal but real workbook: a shared string table plus one
// sheet whose cells reference it, with a deliberate gap at column B.
func buildXLSX(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("xl/sharedStrings.xml", `<?xml version="1.0"?>
<sst><si><t>Property</t></si><si><t>Bid</t></si><si><t>4852 Fountain</t></si></sst>`)
	add("xl/worksheets/sheet1.xml", `<?xml version="1.0"?>
<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2" t="s"><v>2</v></c><c r="C2"><v>38500</v></c></row>
</sheetData></worksheet>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXLSXBecomesTSV(t *testing.T) {
	r := Doc("bids.xlsx", buildXLSX(t))
	if r.Via != "xlsx-xml" || !r.HasText {
		t.Fatalf("via=%q hasText=%v — want a real xlsx extract", r.Via, r.HasText)
	}
	// shared-table indices resolve to their strings, inline numbers survive
	for _, want := range []string{"Property", "Bid", "4852 Fountain", "38500"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("extract missing %q:\n%s", want, r.Text)
		}
	}
	// the gap at column B is preserved, so columns still line up
	if !strings.Contains(r.Text, "Property\t\tBid") {
		t.Errorf("column gap not preserved:\n%q", r.Text)
	}
	if !strings.Contains(r.Text, "## sheet1") {
		t.Errorf("sheet not named:\n%s", r.Text)
	}
}

func TestDOCXStripsMarkup(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(`<w:document><w:body><w:p><w:r><w:t>WM Electric</w:t></w:r>` +
		`<w:tab/><w:r><w:t>&amp; Sons</w:t></w:r></w:p><w:p><w:r><w:t>$38,500</w:t></w:r></w:p></w:body></w:document>`))
	zw.Close()
	r := Doc("bid.docx", buf.Bytes())
	if r.Via != "docx-xml" || !r.HasText {
		t.Fatalf("via=%q hasText=%v", r.Via, r.HasText)
	}
	if !strings.Contains(r.Text, "WM Electric\t& Sons") {
		t.Errorf("tabs/entities not handled: %q", r.Text)
	}
	if !strings.Contains(r.Text, "$38,500") || strings.Contains(r.Text, "<w:") {
		t.Errorf("markup survived or text lost: %q", r.Text)
	}
}

func TestNoTextLayerIsHonest(t *testing.T) {
	// an image has no text — the caller must be able to tell, so it hands the
	// agent the file PATH instead of this marker
	for _, name := range []string{"roof.png", "scan.jpg", "deck.heic"} {
		r := Doc(name, []byte("\x89PNG\r\n\x1a\n"))
		if r.HasText {
			t.Errorf("%s reported a text layer", name)
		}
		if r.Via != "none" {
			t.Errorf("%s via=%q want none", name, r.Via)
		}
	}
	// a corrupt docx does not masquerade as empty prose
	if r := Doc("broken.docx", []byte("not a zip")); r.HasText {
		t.Error("corrupt docx claimed a text layer")
	}
}

func TestVerbatimAndClamp(t *testing.T) {
	if r := Doc("notes.csv", []byte("a,b\n1,2\n")); r.Via != "verbatim" || !strings.Contains(r.Text, "a,b") {
		t.Fatalf("csv passthrough broken: %+v", r)
	}
	// over-long input is cut with a visible marker, on a rune boundary
	long := strings.Repeat("é", MaxLen) // 2 bytes each — forces a mid-rune cut
	r := Doc("big.txt", []byte(long))
	if !strings.Contains(r.Text, "extract truncated at") {
		t.Fatal("no truncation marker")
	}
	if !utf8Valid(r.Text) {
		t.Fatal("truncation split a rune")
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

func TestPDFUsesPdftotextWhenPresent(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	// a minimal one-page PDF with a text layer
	pdf := "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]/Contents 4 0 R" +
		"/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
		"4 0 obj<</Length 44>>stream\nBT /F1 12 Tf 20 100 Td (ROOF BID 38500) Tj ET\nendstream endobj\n" +
		"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n" +
		"trailer<</Root 1 0 R>>\n"
	r := Doc("bid.pdf", []byte(pdf))
	if !r.HasText {
		t.Skipf("poppler declined this hand-built PDF: %q", r.Text)
	}
	if r.Via != "pdftotext" || !strings.Contains(r.Text, "38500") {
		t.Errorf("via=%q text=%q", r.Via, r.Text)
	}
}
