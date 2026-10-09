package server

// What Alfred can look at in a problem chat (2026-10-09):
//   - the problem's own inputs — photos as they are, and each drawing PDF as
//     one image per sheet, labelled with its sheet number (A3.13…) read from
//     the sheet's own text, so a vision model opens exactly the sheet it
//     needs (vision_analyze takes a local path; a model that accepts images
//     sees the pixels directly);
//   - pictures of the model the owner sends with "Check this model" —
//     labelled 3D views and the true section — attached to that message as
//     ordinary chat files.
// Rendered sheets are cached by the input's content revision, so a drawing
// set is rasterised once, never per message.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"manifest/construction"
)

type cxSheet struct {
	Page     int         `json:"page"`
	Label    string      `json:"label,omitempty"`
	Path     string      `json:"path"` // the whole sheet
	Quarters []cxQuarter `json:"quarters,omitempty"`
}

type cxQuarter struct {
	Where string `json:"where"`
	Path  string `json:"path"`
}

type cxInputFile struct {
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Label        string    `json:"label,omitempty"`
	Verification string    `json:"verification"`
	Path         string    `json:"path"`
	Sheets       []cxSheet `json:"sheets,omitempty"`
	Note         string    `json:"note,omitempty"`
}

var cxRenderLocks sync.Map // revision → *sync.Mutex

// sheet numbers as architects write them: A3.13, S2.01, A101…
var cxSheetRE = regexp.MustCompile(`\b([A-Z]{1,2}-?\d{1,2}\.\d{1,2}[A-Z]?)\b`)

const cxMaxSheets = 80

// constructionInputFiles materialises the problem's inputs where the agent
// can open them, rendering each PDF's pages once. The cache holds only what
// rendering produced (keyed by content), never an input's name or role, and
// only a complete render is cached — a failed one is tried again next time.
// wait bounds how long a caller waits for a render another request started:
// the turn's brief must not hold the model up for minutes.
func (s *Server) constructionInputFiles(sub construction.SubjectRef, st *construction.State, wait time.Duration) []cxInputFile {
	var out []cxInputFile
	for _, in := range st.Problem.Inputs {
		f := cxInputFile{Name: in.Name, Role: in.Role, Label: in.Label, Verification: in.Verification}
		r, ok := s.constructionRender(sub, st.Problem.ID, in, wait)
		if !ok {
			f.Note = "the drawing sheets are still being prepared; ask again in a minute to have them"
		}
		f.Path, f.Sheets = r.Path, r.Sheets
		if r.Note != "" {
			f.Note = r.Note
		}
		out = append(out, f)
	}
	return out
}

type cxRendered struct {
	Path   string    `json:"path"`
	Sheets []cxSheet `json:"sheets,omitempty"`
	Note   string    `json:"note,omitempty"`
}

func (s *Server) constructionRender(sub construction.SubjectRef, pid string, in construction.InputRef, wait time.Duration) (cxRendered, bool) {
	var r cxRendered
	dir := filepath.Join(s.construction.store.Root(), "chat-files", in.Revision)
	idx := filepath.Join(dir, "index.json")
	lk, _ := cxRenderLocks.LoadOrStore(in.Revision, &sync.Mutex{})
	mu := lk.(*sync.Mutex)
	deadline := time.Now().Add(wait)
	for !mu.TryLock() {
		if time.Now().After(deadline) {
			return r, false
		}
		time.Sleep(250 * time.Millisecond)
	}
	defer mu.Unlock()
	if b, err := os.ReadFile(idx); err == nil && json.Unmarshal(b, &r) == nil && r.Path != "" {
		return r, true
	}
	data, err := s.construction.store.Content(sub, pid, in.ArtifactID, in.Revision)
	if err != nil {
		return cxRendered{Note: "unreadable: " + err.Error()}, true
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return cxRendered{Note: err.Error()}, true
	}
	ext := strings.ToLower(filepath.Ext(in.Name))
	if ext == "" || len(ext) > 6 {
		ext = map[bool]string{true: ".pdf", false: ".bin"}[strings.Contains(in.Mime, "pdf")]
	}
	r.Path = filepath.Join(dir, "original"+ext)
	if err := os.WriteFile(r.Path, data, 0o600); err != nil {
		return cxRendered{Note: err.Error()}, true
	}
	if strings.Contains(in.Mime, "pdf") {
		r.Sheets, r.Note = renderPDFSheets(r.Path, dir)
	}
	if r.Note == "" {
		if b, err := json.Marshal(r); err == nil {
			_ = os.WriteFile(idx, b, 0o600)
		}
	}
	return r, true
}

// renderPDFSheets renders each page (up to cxMaxSheets) twice over: a whole
// sheet overview, and four sharper quarters with a little overlap — a 36×24
// drawing sheet shrunk to one image loses the dimensions a model needs. Each
// sheet is named by the sheet number in its title block (the bottom-right
// corner's text), else the number that occurs most on the page.
func renderPDFSheets(pdf, dir string) ([]cxSheet, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	info, err := exec.CommandContext(ctx, "pdfinfo", "-f", "1", "-l", fmt.Sprint(cxMaxSheets), pdf).Output()
	if err != nil {
		return nil, "pages could not be read (pdfinfo failed)"
	}
	type pg struct{ w, h float64 }
	var pages []pg
	rot := map[int]int{}
	for _, line := range strings.Split(string(info), "\n") {
		var n, deg int
		var w, h float64
		t := strings.TrimSpace(line)
		if _, err := fmt.Sscanf(t, "Page %d size: %f x %f", &n, &w, &h); err == nil && w > 0 {
			pages = append(pages, pg{w, h})
		} else if _, err := fmt.Sscanf(t, "Page %d rot: %d", &n, &deg); err == nil && len(pages) > 0 {
			rot[len(pages)-1] = deg // pdfinfo prints a page's rot after its size
		}
	}
	// pdftoppm renders a rotated page upright: crop it as rendered
	for i := range pages {
		if d := rot[i] % 180; d == 90 || d == -90 {
			pages[i].w, pages[i].h = pages[i].h, pages[i].w
		}
	}
	if len(pages) == 0 {
		// single-page form of pdfinfo: "Page size: W x H pts"
		for _, line := range strings.Split(string(info), "\n") {
			var w, h float64
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "Page size: %f x %f", &w, &h); err == nil && w > 0 {
				pages = append(pages, pg{w, h})
			}
		}
	}
	var texts []string
	if txt, err := exec.CommandContext(ctx, "pdftotext", "-layout", "-l", fmt.Sprint(cxMaxSheets), pdf, "-").Output(); err == nil {
		texts = strings.Split(string(txt), "\f")
	}
	var sheets []cxSheet
	for i, p := range pages {
		n := fmt.Sprint(i + 1)
		base := filepath.Join(dir, fmt.Sprintf("sheet-%02d", i+1))
		if out, err := exec.CommandContext(ctx, "pdftoppm", "-f", n, "-l", n, "-png", "-singlefile", "-scale-to", "2000", pdf, base).CombinedOutput(); err != nil {
			return sheets, "page " + n + " could not be rendered (" + strings.TrimSpace(string(out)) + ")"
		}
		sh := cxSheet{Page: i + 1, Path: base + ".png"}
		// quarters at 150 dpi, 6% overlap so nothing is cut at a seam
		const dpi = 150.0
		W, H := p.w/72*dpi, p.h/72*dpi
		qw, qh := int(W*0.56), int(H*0.56)
		for k, q := range []struct {
			name string
			x, y int
		}{{"top-left", 0, 0}, {"top-right", int(W) - qw, 0}, {"bottom-left", 0, int(H) - qh}, {"bottom-right", int(W) - qw, int(H) - qh}} {
			qp := fmt.Sprintf("%s-q%d", base, k+1)
			if _, err := exec.CommandContext(ctx, "pdftoppm", "-f", n, "-l", n, "-png", "-singlefile", "-r", "150",
				"-x", fmt.Sprint(q.x), "-y", fmt.Sprint(q.y), "-W", fmt.Sprint(qw), "-H", fmt.Sprint(qh), pdf, qp).CombinedOutput(); err == nil {
				sh.Quarters = append(sh.Quarters, cxQuarter{Where: q.name, Path: qp + ".png"})
			}
		}
		// the title block: bottom-right corner text
		x, y := int(p.w*0.8), int(p.h*0.78)
		if tb, err := exec.CommandContext(ctx, "pdftotext", "-f", n, "-l", n, "-x", fmt.Sprint(x), "-y", fmt.Sprint(y),
			"-W", fmt.Sprint(int(p.w)-x), "-H", fmt.Sprint(int(p.h)-y), pdf, "-").Output(); err == nil {
			if m := cxSheetRE.FindAllString(string(tb), -1); len(m) > 0 {
				sh.Label = m[len(m)-1]
			}
		}
		if sh.Label == "" && i < len(texts) {
			if m := cxSheetRE.FindAllString(texts[i], -1); len(m) > 0 {
				sh.Label = cxMostFrequent(m)
			}
		}
		sheets = append(sheets, sh)
	}
	return sheets, ""
}

// the sheet's own number appears in its title block and its references;
// the most frequent token on the page is the best guess, ties to the last
func cxMostFrequent(xs []string) string {
	n := map[string]int{}
	best := ""
	for _, x := range xs {
		n[x]++
		if n[x] >= n[best] {
			best = x
		}
	}
	return best
}

func (s *Server) constructionInputsBrief(sub construction.SubjectRef, st *construction.State) string {
	files := s.constructionInputFiles(sub, st, 90*time.Second) // a render already under way gets a minute and a half
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThe owner's documents and photos for this problem. Open them with vision_analyze (it takes these local paths) — drawings are one image per sheet; read the sheets that bear on the junction before proposing sizes, and say which sheet a number came from:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s (%s%s; %s)", f.Name, f.Role, map[bool]string{true: ": " + f.Label, false: ""}[f.Label != ""], strings.ReplaceAll(f.Verification, "-", " "))
		if f.Note != "" {
			b.WriteString(" — " + f.Note)
		}
		if len(f.Sheets) == 0 {
			fmt.Fprintf(&b, ": %s\n", f.Path)
			continue
		}
		b.WriteString(":\n")
		for _, sh := range f.Sheets {
			label := sh.Label
			if label == "" {
				label = "unlabelled"
			}
			fmt.Fprintf(&b, "  - page %d, sheet %s — whole sheet: %s\n", sh.Page, label, sh.Path)
			for _, q := range sh.Quarters {
				fmt.Fprintf(&b, "      %s quarter (sharper, for reading dimensions): %s\n", q.Where, q.Path)
			}
		}
	}
	return b.String()
}

// ---- pictures sent with a message ----------------------------------------------

type constructionChatImage struct {
	Name string `json:"name"`
	Data string `json:"data"` // base64 PNG/JPEG
}

const cxMaxImageBytes = 6 << 20

// constructionAttachImages stores the pictures as chat files of this
// conversation and returns the tokens the prompt composer turns into paths.
func (s *Server) constructionAttachImages(agent, conv string, imgs []constructionChatImage) (string, error) {
	if len(imgs) == 0 {
		return "", nil
	}
	if s.chatFilesRoot == "" {
		return "", construction.Unavailable("chat files are not wired here")
	}
	if len(imgs) > 6 {
		return "", construction.Invalid("send at most six pictures")
	}
	owner := "agent:" + agent + "/" + conv
	chatFilesMu.Lock()
	defer chatFilesMu.Unlock()
	var tokens strings.Builder
	for _, im := range imgs {
		data, err := base64.StdEncoding.DecodeString(im.Data)
		if err != nil || len(data) == 0 || len(data) > cxMaxImageBytes {
			return "", construction.Invalid("each picture must be a base64 PNG or JPEG under 6 MB")
		}
		typ := http.DetectContentType(data)
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg"}[typ]
		if ext == "" {
			return "", construction.Invalid("pictures must be PNG or JPEG")
		}
		name := sanitizeUploadName(strings.TrimSuffix(im.Name, filepath.Ext(im.Name)) + ext)
		if name == "" {
			name = "model" + ext
		}
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		id := hex.EncodeToString(raw[:])
		dir := filepath.Join(s.chatFilesRoot, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		f := ownedChatFile{ID: id, Name: name, Size: int64(len(data)), Type: typ, Owner: owner, Created: time.Now().UTC(), Sent: true}
		if err := os.WriteFile(filepath.Join(dir, "content"+ext), data, 0o600); err != nil {
			return "", err
		}
		if err := s.saveOwnedFile(f); err != nil {
			return "", err
		}
		tokens.WriteString("\n[context-file:: " + id + "]")
	}
	return tokens.String(), nil
}
