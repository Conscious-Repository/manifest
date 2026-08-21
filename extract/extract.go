// Package extract turns an uploaded document into the text an agent can read.
//
// It was the RE intake ritual's private helper; chat attachments need exactly
// the same thing, so it moved here rather than being written twice. Dispatch is
// by filename extension — the caller has already checked that the extension
// agrees with the sniffed content type, so the extension is trustworthy by the
// time we see it.
//
// Nothing here shells out except pdftotext, and nothing here is a new
// dependency: docx and xlsx are both zip+XML, which the standard library reads.
package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MaxLen bounds an extract destined for a VAULT NOTE — 55k leaves room under
// the agents' 60k read cap for the frontmatter and the header line.
const MaxLen = 55000

// MaxStored bounds an extract written as a plain file for an agent to open.
// Nothing reads it through the 60k cap, so the only reason to bound it at all
// is to keep one pathological document from filling a disk.
const MaxStored = 4 << 20

// pdfTimeout bounds pdftotext. It had none: a PDF crafted to make poppler spin
// would have hung the HTTP request that uploaded it.
const pdfTimeout = 30 * time.Second

// Result is what one document yielded.
type Result struct {
	Text string // the extracted text, capped at MaxLen
	Via  string // pdftotext | docx-xml | xlsx-xml | verbatim | none
	// HasText is false when the document carries no text layer we can read
	// (an image, or a scan). Callers show the file to people and hand the
	// agent the PATH instead of pretending there is nothing there.
	HasText bool
}

// Doc extracts text bounded for a vault note. Use DocLimit when the text is
// going somewhere that can hold more.
func Doc(name string, data []byte) Result { return DocLimit(name, data, MaxLen) }

// DocLimit extracts text from a document's bytes, dispatching on its filename.
func DocLimit(name string, data []byte, limit int) Result {
	clamp := func(s string) string { return clampTo(s, limit) }
	switch strings.ToLower(path.Ext(name)) {
	case ".pdf":
		if txt, ok := pdfText(data); ok {
			return Result{Text: clamp(txt), Via: "pdftotext", HasText: true}
		}
		return Result{Text: "(no text layer — this PDF is probably a scan, or pdftotext is not installed)", Via: "none"}
	case ".docx":
		if txt := docxText(data); strings.TrimSpace(txt) != "" {
			return Result{Text: clamp(txt), Via: "docx-xml", HasText: true}
		}
		return Result{Text: "(no text could be extracted from this docx)", Via: "none"}
	case ".xlsx":
		if txt := xlsxText(data); strings.TrimSpace(txt) != "" {
			return Result{Text: clamp(txt), Via: "xlsx-xml", HasText: true}
		}
		return Result{Text: "(no cells could be read from this spreadsheet)", Via: "none"}
	case ".txt", ".md", ".csv", ".tsv", ".json", ".log":
		return Result{Text: clamp(string(data)), Via: "verbatim", HasText: true}
	default:
		return Result{Text: "(no text layer for this file type)", Via: "none"}
	}
}

func clampTo(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// cut on a rune boundary — a split multi-byte rune renders as garbage
	cut := limit
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "\n\n[extract truncated at " + strconv.Itoa(limit) + " chars]"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// --- pdf -------------------------------------------------------------------

func pdfText(data []byte) (string, bool) {
	pt, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), pdfTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, pt, "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(data)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", false
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", false
	}
	return out.String(), true
}

// --- docx ------------------------------------------------------------------

var tagRe = regexp.MustCompile(`<[^>]+>`)

// docxText unzips word/document.xml and strips the markup, preserving
// paragraph and tab structure well enough to read.
func docxText(data []byte) string {
	f := zipMember(data, func(n string) bool { return n == "word/document.xml" })
	if f == "" {
		return ""
	}
	s := strings.ReplaceAll(f, "</w:p>", "\n")
	s = strings.ReplaceAll(s, "<w:tab/>", "\t")
	s = strings.ReplaceAll(s, "<w:br/>", "\n")
	return unescape(tagRe.ReplaceAllString(s, ""))
}

// --- xlsx ------------------------------------------------------------------
//
// A workbook is zip+XML like docx. Cell values are mostly indices into one
// shared string table; numbers sit inline. We emit TSV per sheet, which is
// what a model reads best and what a person would paste into a spreadsheet.

func xlsxText(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	shared := sharedStrings(zr)
	var sheets []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheets = append(sheets, f.Name)
		}
	}
	sort.Strings(sheets)
	var b strings.Builder
	for _, name := range sheets {
		raw := memberOf(zr, name)
		if raw == "" {
			continue
		}
		rows := sheetRows(raw, shared)
		if len(rows) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s\n", strings.TrimSuffix(path.Base(name), ".xml"))
		for _, r := range rows {
			b.WriteString(strings.Join(r, "\t"))
			b.WriteString("\n")
		}
		if b.Len() > MaxStored {
			break
		}
	}
	return b.String()
}

// sharedStrings reads xl/sharedStrings.xml into the index table cells refer to.
func sharedStrings(zr *zip.Reader) []string {
	raw := memberOf(zr, "xl/sharedStrings.xml")
	if raw == "" {
		return nil
	}
	var out []string
	dec := xml.NewDecoder(strings.NewReader(raw))
	var cur strings.Builder
	inSI := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				inSI, cur = true, strings.Builder{}
			}
		case xml.CharData:
			if inSI {
				cur.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "si" {
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out
}

var cellRefRe = regexp.MustCompile(`^([A-Z]+)`)

// sheetRows walks a worksheet's cells into TSV rows, honouring column letters
// so a gap in the middle of a row stays a gap.
func sheetRows(raw string, shared []string) [][]string {
	dec := xml.NewDecoder(strings.NewReader(raw))
	var rows [][]string
	var row []string
	var cellType, cellRef string
	var val strings.Builder
	inV := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = nil
			case "c":
				cellType, cellRef, val = "", "", strings.Builder{}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "t":
						cellType = a.Value
					case "r":
						cellRef = a.Value
					}
				}
			case "v", "t":
				inV = true
			}
		case xml.CharData:
			if inV {
				val.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inV = false
			case "c":
				text := val.String()
				if cellType == "s" { // an index into the shared table
					if i, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && i >= 0 && i < len(shared) {
						text = shared[i]
					}
				}
				if col := colIndex(cellRef); col >= 0 {
					for len(row) < col {
						row = append(row, "")
					}
				}
				row = append(row, strings.TrimSpace(text))
			case "row":
				if len(row) > 0 {
					rows = append(rows, row)
				}
				row = nil
			}
		}
	}
	return rows
}

// colIndex turns a cell ref ("C7") into a 0-based column (-1 when absent).
func colIndex(ref string) int {
	m := cellRefRe.FindStringSubmatch(strings.ToUpper(ref))
	if m == nil {
		return -1
	}
	n := 0
	for _, c := range m[1] {
		n = n*26 + int(c-'A') + 1
	}
	return n - 1
}

// --- shared zip helpers -----------------------------------------------------

func memberOf(zr *zip.Reader, name string) string {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 16<<20)) // bounds a zip bomb
		rc.Close()
		if err != nil {
			return ""
		}
		return string(raw)
	}
	return ""
}

func zipMember(data []byte, match func(string) bool) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		if match(f.Name) {
			return memberOf(zr, f.Name)
		}
	}
	return ""
}

func unescape(s string) string {
	return strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'",
	).Replace(s)
}
