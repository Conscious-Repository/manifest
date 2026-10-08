package server

// Construction exports: same-version derivatives of one assembly revision —
// GLB, the section as SVG or vector PDF, or the redacted detail package —
// retained as private derived artifacts with a record tying each to the exact
// revision, generator and geometry hash; and an uncommitted section preview
// (SVG/JSON) for the 2D view. Exports are explicit downloads; there is no
// publish endpoint and no public URL.

import (
	"net/http"
	"strconv"

	"manifest/construction"
)

func (s *Server) registerConstructionExportRoutes(mux *http.ServeMux, p string) {
	mux.HandleFunc("POST "+p+"/problems/{id}/assemblies/{asm}/exports", s.handleConstructionExport)
	mux.HandleFunc("GET "+p+"/problems/{id}/assemblies/{asm}/section", s.handleConstructionSection)
}

type constructionExportRequest struct {
	SchemaVersion int                        `json:"schemaVersion"`
	RequestID     string                     `json:"requestId"`
	Revision      string                     `json:"revision"`
	Format        string                     `json:"format"` // glb | svg | pdf | package
	Section       *construction.SectionPlane `json:"section,omitempty"`
	Paper         string                     `json:"paper,omitempty"`
	Scale         float64                    `json:"scale,omitempty"`
	Window        *[4]float64                `json:"window,omitempty"`
}

func (s *Server) handleConstructionExport(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	body, ok := readConstructionBody(w, r, construction.MaxCommandBytes)
	if !ok {
		return
	}
	var req constructionExportRequest
	if err := construction.DecodeRequest(body, &req); err != nil {
		constructionError(w, err)
		return
	}
	if req.SchemaVersion != 1 || !construction.ValidRequestID(req.RequestID) || !construction.ValidToken(req.Revision) {
		constructionError(w, construction.Invalid("schemaVersion 1, requestId and the exact assembly revision are required"))
		return
	}
	if req.Format != "glb" && req.Format != "svg" && req.Format != "pdf" && req.Format != "package" {
		constructionError(w, construction.Invalid("format must be glb, svg, pdf or package (PNG is not generated server-side)"))
		return
	}
	id, asmID := r.PathValue("id"), r.PathValue("asm")
	payload, err := construction.CanonicalizeJSON(body)
	if err != nil {
		constructionError(w, construction.Invalid(err.Error()))
		return
	}
	payloadHash := construction.Token(payload)
	// an identical replay returns the original record without regenerating
	if rc, err := s.construction.store.Receipt(sub, id, req.RequestID); err == nil {
		if rc.PayloadHash != payloadHash {
			constructionError(w, construction.Conflict("request ID was already used for different content", nil))
			return
		}
		st, err := s.construction.store.Load(sub, id)
		if err != nil {
			constructionError(w, err)
			return
		}
		s.respondExport(w, sub, st, rc, req.RequestID)
		return
	}
	a, tok, err := s.construction.store.AssemblyAt(sub, id, asmID, req.Revision)
	if err != nil {
		constructionError(w, err) // a revision outside this assembly's history is refused
		return
	}
	st, err := s.construction.store.Load(sub, id)
	if err != nil {
		constructionError(w, err)
		return
	}
	rep, err := s.construction.store.ValidationAt(sub, id, asmID, tok)
	if err != nil {
		rep = nil
	}
	opt := construction.DrawingOptions{Paper: orStr(req.Paper, "A3"), Scale: req.Scale}
	if opt.Scale == 0 {
		opt.Scale = 5
	}
	if req.Window != nil {
		opt.Window = *req.Window
	}
	var data []byte
	var name, generator, geomHash string
	params := map[string]string{"format": req.Format}
	switch req.Format {
	case "glb":
		ir, err := construction.Compile(a, st.Catalog)
		if err != nil {
			constructionError(w, err)
			return
		}
		data, err = construction.GLB(ir, map[string]string{"problemId": id, "assemblyId": a.ID, "assemblyRevision": tok, "assemblyName": a.Name})
		if err != nil {
			constructionError(w, err)
			return
		}
		name, generator, geomHash = a.ID+"-"+tok[:12]+".glb", construction.GLBGenerator, ir.Hash
	case "svg", "pdf":
		d, sec, ir, err := construction.DrawingFor(st, a, tok, rep, req.Section, opt)
		if err != nil {
			constructionError(w, err)
			return
		}
		title := a.Name + " — section"
		if req.Format == "svg" {
			data = d.SVG(title)
		} else {
			data = d.PDF(title)
		}
		name, generator, geomHash = a.ID+"-"+tok[:12]+"-section."+req.Format, construction.DrawingGenerator, ir.Hash
		params["paper"], params["scale"] = d.Paper, "1:"+strconv.FormatFloat(d.Scale, 'f', -1, 64)
		params["plane"] = planeString(sec.Plane)
	case "package":
		z, man, err := construction.DetailPackage(st, a, tok, rep, construction.PackageOptions{Plane: req.Section, Drawing: opt})
		if err != nil {
			constructionError(w, err)
			return
		}
		data, name, generator, geomHash = z, a.ID+"-"+tok[:12]+"-detail-package.zip", construction.PackageGenerator, man.GeometryHash
		params["paper"], params["scale"] = opt.Paper, "1:"+strconv.FormatFloat(opt.Scale, 'f', -1, 64)
	}
	rc := (*construction.Receipt)(nil)
	_, rc, err = s.construction.store.Commit(sub, id, construction.CommitRequest{RequestID: req.RequestID, PayloadHash: payloadHash, Actor: actor}, func(tx *construction.Tx) error {
		artifactID, rev, err := tx.Retain("construction-derived", name, data)
		if err != nil {
			return err
		}
		if tx.Next.Derived == nil {
			tx.Next.Derived = &construction.DerivedBundle{Envelope: construction.Envelope{SchemaVersion: 1, Kind: construction.DocDerived, ID: id}, ProblemID: id, Artifacts: []construction.DerivedRecord{}}
		}
		rec := construction.DerivedRecord{ArtifactID: artifactID, Revision: rev, Format: req.Format, Name: name, AssemblyID: a.ID, AssemblyRevision: tok,
			GeometryHash: geomHash, Generator: generator + " · " + construction.CompilerVersion, Parameters: params, InputHashes: []string{tok}, Size: int64(len(data)),
			CreatedAt: tx.Now.Format("2006-01-02T15:04:05.999999999Z07:00"), RequestID: req.RequestID}
		tx.Next.Derived.Artifacts = append(tx.Next.Derived.Artifacts, rec)
		tx.Record("Export", "derived/"+artifactID, nil, rec)
		tx.Output(rev)
		tx.ViewOnly() // a derivative changes no physical model
		tx.Summary("exported " + req.Format)
		tx.Event("construction.export", req.Format+" of "+a.Name)
		return nil
	})
	if err != nil {
		constructionError(w, err)
		return
	}
	st, err = s.construction.store.Load(sub, id)
	if err != nil {
		constructionError(w, err)
		return
	}
	s.respondExport(w, sub, st, rc, req.RequestID)
}

func (s *Server) respondExport(w http.ResponseWriter, sub construction.SubjectRef, st *construction.State, rc *construction.Receipt, requestID string) {
	if st.Derived != nil {
		for _, rec := range st.Derived.Artifacts {
			if rec.RequestID == requestID {
				url := constructionPrefix(sub) + "/problems/" + st.Problem.ID + "/artifacts/" + rec.ArtifactID + "?revision=" + rec.Revision + "&download=1"
				constructionJSON(w, map[string]any{"record": rec, "url": url, "receipt": rc})
				return
			}
		}
	}
	constructionError(w, construction.NotFound("export record missing"))
}

func constructionPrefix(sub construction.SubjectRef) string {
	if sub.Kind == construction.SubjectHome {
		return "/api/home/construction"
	}
	return "/api/properties/" + sub.ID + "/construction"
}

func planeString(p construction.SectionPlane) string {
	f := func(v [3]float64) string {
		return strconv.FormatFloat(v[0], 'f', -1, 64) + "," + strconv.FormatFloat(v[1], 'f', -1, 64) + "," + strconv.FormatFloat(v[2], 'f', -1, 64)
	}
	return "origin=" + f(p.Origin) + ";normal=" + f(p.Normal) + ";up=" + f(p.Up)
}

func qfloat(r *http.Request, key string, def float64) (float64, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, true
	}
	f, err := strconv.ParseFloat(v, 64)
	return f, err == nil
}

// handleConstructionSection draws (does not retain) a section of one exact
// revision: SVG for the 2D view (served as an inert image), or JSON.
func (s *Server) handleConstructionSection(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	id, asmID := r.PathValue("id"), r.PathValue("asm")
	a, tok, err := s.construction.store.AssemblyAt(sub, id, asmID, r.URL.Query().Get("revision"))
	if err != nil {
		constructionError(w, err)
		return
	}
	st, err := s.construction.store.Load(sub, id)
	if err != nil {
		constructionError(w, err)
		return
	}
	var plane *construction.SectionPlane
	if r.URL.Query().Get("nx") != "" {
		vals := map[string]float64{}
		for _, k := range []string{"ox", "oy", "oz", "nx", "ny", "nz", "ux", "uy", "uz"} {
			f, ok := qfloat(r, k, 0)
			if !ok {
				constructionError(w, construction.Invalid("section parameter "+k+" must be a number"))
				return
			}
			vals[k] = f
		}
		plane = &construction.SectionPlane{Origin: [3]float64{vals["ox"], vals["oy"], vals["oz"]}, Normal: [3]float64{vals["nx"], vals["ny"], vals["nz"]}, Up: [3]float64{vals["ux"], vals["uy"], vals["uz"]}, Enabled: true}
	}
	scale, ok1 := qfloat(r, "scale", 5)
	if !ok1 {
		constructionError(w, construction.Invalid("scale must be a number"))
		return
	}
	rep, _ := s.construction.store.ValidationAt(sub, id, asmID, tok)
	d, sec, _, err := construction.DrawingFor(st, a, tok, rep, plane, construction.DrawingOptions{Paper: orStr(r.URL.Query().Get("paper"), "A3"), Scale: scale})
	if err != nil {
		constructionError(w, err)
		return
	}
	if r.URL.Query().Get("format") == "json" {
		constructionJSON(w, map[string]any{"assemblyRevision": tok, "section": sec, "drawing": map[string]any{"paper": d.Paper, "scale": d.Scale, "window": d.Window}})
		return
	}
	svg := d.SVG(a.Name + " — section")
	if err := construction.CheckSVG(svg); err != nil {
		constructionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Geometry-Hash", sec.GeometryHash)
	_, _ = w.Write(svg)
}
