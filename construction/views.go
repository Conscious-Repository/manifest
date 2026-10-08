package construction

// Persistent views and annotations (§3.2 View/Annotation, §5). A view holds
// camera, bookmarks, the section plane, selection, visibility, exploded and
// render mode — renderer state only; it never changes physical validation or
// a bill of materials. Annotations anchor on semantic component ids, never on
// mesh indices; an anchor whose component disappears reads as unresolved.

import (
	"math"
	"strings"
)

func init() {
	registerOp("SaveView", TargetView, false, func() Operation { return &SaveView{} })
	registerOp("SetVisibility", TargetView, false, func() Operation { return &SetVisibility{} })
	registerOp("AddAnnotation", TargetView, false, func() Operation { return &AddAnnotation{} })
	registerOp("RemoveAnnotation", TargetView, false, func() Operation { return &RemoveAnnotation{} })
}

// ViewState is the client-editable part of a view.
type ViewState struct {
	Name         string        `json:"name"`
	AssemblyID   string        `json:"assemblyId"`
	Camera       Camera        `json:"camera"`
	Bookmarks    []Bookmark    `json:"bookmarks"`
	Section      *SectionPlane `json:"section"`
	Selection    []string      `json:"selection"`
	Hidden       []string      `json:"hidden"`
	Isolated     []string      `json:"isolated"`
	Transparent  []string      `json:"transparent"`
	Exploded     float64       `json:"exploded"`
	Mode         string        `json:"mode"`
	Overlays     Overlays      `json:"overlays"`
	Measurements []Measurement `json:"measurements"`
}

func checkCamera(c Camera) []string {
	var out []string
	if c.Projection != "perspective" && c.Projection != "orthographic" {
		out = append(out, "camera projection must be perspective or orthographic")
	}
	vals := append(append(append([]float64{}, c.Position[:]...), c.Target[:]...), c.Up[:]...)
	vals = append(vals, c.FOV, c.Zoom)
	if !finite(vals...) {
		out = append(out, "camera values must be finite")
	}
	for _, v := range vals {
		if math.Abs(v) > 1e6 {
			out = append(out, "camera values out of range")
			break
		}
	}
	if c.FOV < 0 || c.FOV > 170 || c.Zoom < 0 || c.Zoom > 1e4 {
		out = append(out, "camera fov/zoom out of range")
	}
	return out
}

func checkSection(s *SectionPlane) []string {
	if s == nil {
		return nil
	}
	vals := append(append(append([]float64{}, s.Origin[:]...), s.Normal[:]...), s.Up[:]...)
	if !finite(vals...) {
		return []string{"section values must be finite"}
	}
	n := V3(s.Normal)
	u := V3(s.Up)
	if vlen(n) < 1e-9 || vlen(vcross(n, u)) < 1e-6 {
		return []string{"section needs a non-zero normal and an up vector not parallel to it"}
	}
	for _, v := range s.Origin {
		if math.Abs(v) > 1e6 {
			return []string{"section origin out of range"}
		}
	}
	return nil
}

func (v *ViewState) check() []string {
	out := checkText("name", v.Name, 120, false)
	if !ValidID(KindAssembly, v.AssemblyID) {
		out = append(out, "assemblyId must be an asm- id")
	}
	out = append(out, checkCamera(v.Camera)...)
	if len(v.Bookmarks) > 32 {
		out = append(out, "at most 32 bookmarks")
	}
	for _, b := range v.Bookmarks {
		out = append(out, checkText("bookmark name", b.Name, 80, true)...)
		out = append(out, checkCamera(b.Camera)...)
	}
	out = append(out, checkSection(v.Section)...)
	for _, set := range [][]string{v.Selection, v.Hidden, v.Isolated, v.Transparent} {
		if len(set) > MaxComponents {
			out = append(out, "id lists are bounded by the component budget")
		}
		for _, id := range set {
			if !ValidID(KindComponent, id) {
				out = append(out, "view id lists hold component ids")
				break
			}
		}
	}
	if !finite(v.Exploded) || v.Exploded < 0 || v.Exploded > 1 {
		out = append(out, "exploded must be 0–1")
	}
	if v.Mode != "technical" && v.Mode != "realistic" {
		out = append(out, "mode must be technical or realistic")
	}
	if len(v.Measurements) > 64 {
		out = append(out, "at most 64 measurements")
	}
	for _, m := range v.Measurements {
		if !finite(append(append([]float64{}, m.A[:]...), m.B[:]...)...) {
			out = append(out, "measurement points must be finite")
		}
		out = append(out, checkText("measurement label", m.Label, 80, false)...)
	}
	return out
}

// SaveView creates (expectedRevision "") or updates a view. Measurements are
// recomputed from their canonical points, never trusted from the client.
type SaveView struct {
	Op               string    `json:"op"`
	ViewID           string    `json:"viewId"`
	ExpectedRevision string    `json:"expectedRevision"`
	View             ViewState `json:"view"`
}

func (o *SaveView) Name() string { return "SaveView" }
func (o *SaveView) Check() []string {
	out := o.View.check()
	if !ValidID(KindView, o.ViewID) {
		out = append(out, "viewId must be a view- id")
	}
	if o.ExpectedRevision != "" && !ValidToken(o.ExpectedRevision) {
		out = append(out, "expectedRevision must be a revision token or empty for a new view")
	}
	return out
}

func viewCAS(tx *Tx, viewID, expected string) (*View, error) {
	cur := tx.Base.Revision("view:" + viewID)
	if cur != expected {
		return nil, Conflict("the view changed since it was loaded", map[string]string{"view:" + viewID: cur})
	}
	return tx.Next.Views[viewID], nil
}

func (o *SaveView) Apply(tx *Tx, c *ApplyContext) error {
	v, err := viewCAS(tx, o.ViewID, o.ExpectedRevision)
	if err != nil {
		return err
	}
	a := tx.Next.Assemblies[o.View.AssemblyID]
	if a == nil {
		return NotFound("the view's assembly is not in this problem")
	}
	if v == nil {
		v = &View{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocView, ID: o.ViewID}, ProblemID: tx.Next.Problem.ID, Annotations: []Annotation{}}
		tx.Next.Views[o.ViewID] = v
		if tx.Next.Problem.LatestView == "" {
			tx.Next.Problem.LatestView = o.ViewID
		}
	}
	s := o.View
	v.Name = strings.TrimSpace(s.Name)
	v.Assembly = VersionRef{ID: a.ID, Revision: tx.Base.Revision("assembly:" + a.ID)}
	v.Camera = s.Camera
	v.Bookmarks = nonNilBookmarks(s.Bookmarks)
	v.Section = s.Section
	v.Selection = sortedUnique(s.Selection)
	v.Hidden = sortedUnique(s.Hidden)
	v.Isolated = sortedUnique(s.Isolated)
	v.Transparent = sortedUnique(s.Transparent)
	v.Exploded = math.Round(s.Exploded*1000) / 1000
	v.Mode = s.Mode
	v.Overlays = s.Overlays
	v.Measurements = []Measurement{}
	for _, m := range s.Measurements {
		m.Distance = math.Round(vlen(vsub(V3(m.B), V3(m.A)))*1000) / 1000
		v.Measurements = append(v.Measurements, m)
	}
	tx.Record(o.Name(), "view:"+o.ViewID, nil, map[string]any{"assembly": v.Assembly, "mode": v.Mode})
	return nil
}

func nonNilBookmarks(b []Bookmark) []Bookmark {
	if b == nil {
		return []Bookmark{}
	}
	return b
}

// SetVisibility changes one visibility set of a view.
type SetVisibility struct {
	Op               string   `json:"op"`
	ViewID           string   `json:"viewId"`
	ExpectedRevision string   `json:"expectedRevision"`
	ComponentIDs     []string `json:"componentIds"`
	State            string   `json:"state"` // visible | hidden | isolated | transparent | opaque
}

func (o *SetVisibility) Name() string { return "SetVisibility" }
func (o *SetVisibility) Check() []string {
	var out []string
	if !ValidID(KindView, o.ViewID) || !ValidToken(o.ExpectedRevision) {
		out = append(out, "viewId and expectedRevision are required")
	}
	switch o.State {
	case "visible", "hidden", "isolated", "transparent", "opaque":
	default:
		out = append(out, "state must be visible, hidden, isolated, transparent or opaque")
	}
	for _, id := range o.ComponentIDs {
		if !ValidID(KindComponent, id) {
			out = append(out, "componentIds must be cmp- ids")
		}
	}
	return out
}
func (o *SetVisibility) Apply(tx *Tx, c *ApplyContext) error {
	v, err := viewCAS(tx, o.ViewID, o.ExpectedRevision)
	if err != nil {
		return err
	}
	if v == nil {
		return NotFound("no such view")
	}
	remove := func(set, ids []string) []string {
		drop := map[string]bool{}
		for _, id := range ids {
			drop[id] = true
		}
		out := []string{}
		for _, id := range set {
			if !drop[id] {
				out = append(out, id)
			}
		}
		return out
	}
	switch o.State {
	case "visible":
		v.Hidden = remove(v.Hidden, o.ComponentIDs)
		v.Isolated = []string{}
	case "hidden":
		v.Hidden = sortedUnique(append(v.Hidden, o.ComponentIDs...))
	case "isolated":
		v.Isolated = sortedUnique(o.ComponentIDs)
	case "transparent":
		v.Transparent = sortedUnique(append(v.Transparent, o.ComponentIDs...))
	case "opaque":
		v.Transparent = remove(v.Transparent, o.ComponentIDs)
	}
	tx.Record(o.Name(), "view:"+o.ViewID, o.State, o.ComponentIDs)
	return nil
}

// AddAnnotation pins a note to a semantic component (+ optional canonical
// point in world mm).
type AddAnnotation struct {
	Op               string     `json:"op"`
	ViewID           string     `json:"viewId"`
	ExpectedRevision string     `json:"expectedRevision"`
	Annotation       Annotation `json:"annotation"`
}

func (o *AddAnnotation) Name() string { return "AddAnnotation" }
func (o *AddAnnotation) Check() []string {
	out := checkText("text", o.Annotation.Text, 2000, true)
	if !ValidID(KindView, o.ViewID) || !ValidToken(o.ExpectedRevision) {
		out = append(out, "viewId and expectedRevision are required")
	}
	if !ValidID(KindAnnotation, o.Annotation.ID) || !ValidID(KindComponent, o.Annotation.ComponentID) {
		out = append(out, "annotation needs a new ann- id and a component anchor")
	}
	if p := o.Annotation.Point; p != nil && !finite(p[:]...) {
		out = append(out, "annotation point must be finite")
	}
	return out
}
func (o *AddAnnotation) Apply(tx *Tx, c *ApplyContext) error {
	v, err := viewCAS(tx, o.ViewID, o.ExpectedRevision)
	if err != nil {
		return err
	}
	if v == nil {
		return NotFound("no such view")
	}
	a := tx.Next.Assemblies[v.Assembly.ID]
	if a == nil {
		return NotFound("the view's assembly is gone")
	}
	if _, ok := a.Component(o.Annotation.ComponentID); !ok {
		return Invalid("annotations anchor on a component of the view's assembly")
	}
	for _, an := range v.Annotations {
		if an.ID == o.Annotation.ID {
			return Invalid("annotation id already used")
		}
	}
	ev := map[string]bool{}
	if tx.Next.Evidence != nil {
		for _, e := range tx.Next.Evidence.Evidence {
			ev[e.ID] = true
		}
	}
	for _, id := range o.Annotation.EvidenceIDs {
		if !ev[id] {
			return Invalid("annotation evidence " + id + " is not in this problem")
		}
	}
	an := o.Annotation
	an.Author = tx.Actor
	an.CreatedAt = tx.Now.Format("2006-01-02T15:04:05.999999999Z07:00")
	an.State = "active"
	an.EvidenceIDs = sortedUnique(an.EvidenceIDs)
	an.Text = strings.TrimSpace(an.Text)
	v.Annotations = append(v.Annotations, an)
	tx.Record(o.Name(), "view:"+o.ViewID+"/"+an.ID, nil, an)
	return nil
}

// RemoveAnnotation marks an annotation deleted (kept for history).
type RemoveAnnotation struct {
	Op               string `json:"op"`
	ViewID           string `json:"viewId"`
	ExpectedRevision string `json:"expectedRevision"`
	AnnotationID     string `json:"annotationId"`
}

func (o *RemoveAnnotation) Name() string { return "RemoveAnnotation" }
func (o *RemoveAnnotation) Check() []string {
	if !ValidID(KindView, o.ViewID) || !ValidToken(o.ExpectedRevision) || !ValidID(KindAnnotation, o.AnnotationID) {
		return []string{"viewId, expectedRevision and annotationId are required"}
	}
	return nil
}
func (o *RemoveAnnotation) Apply(tx *Tx, c *ApplyContext) error {
	v, err := viewCAS(tx, o.ViewID, o.ExpectedRevision)
	if err != nil {
		return err
	}
	if v == nil {
		return NotFound("no such view")
	}
	for i := range v.Annotations {
		if v.Annotations[i].ID == o.AnnotationID {
			v.Annotations[i].State = "deleted"
			tx.Record(o.Name(), "view:"+o.ViewID+"/"+o.AnnotationID, "active", "deleted")
			return nil
		}
	}
	return NotFound("no such annotation")
}

// ResolveAnnotations marks annotations whose component no longer exists in
// an assembly revision as unresolved (a read-time projection).
func ResolveAnnotations(v *View, a *Assembly) []Annotation {
	out := make([]Annotation, 0, len(v.Annotations))
	for _, an := range v.Annotations {
		if an.State == "active" {
			if _, ok := a.Component(an.ComponentID); !ok {
				an.State = "unresolved"
			}
		}
		out = append(out, an)
	}
	return out
}

func init() {
	docValidators[DocView] = func(st *State, key string, doc any) error {
		v := doc.(*View)
		out := checkEnvelope(v.Envelope, DocView, KindView)
		if st.Problem == nil || v.ProblemID != st.Problem.ID {
			out = append(out, "view problemId must be the owning problem")
		}
		if st.Assemblies[v.Assembly.ID] == nil || !ValidToken(v.Assembly.Revision) {
			out = append(out, "view must pin an assembly revision of this problem")
		}
		state := ViewState{Name: v.Name, AssemblyID: v.Assembly.ID, Camera: v.Camera, Bookmarks: v.Bookmarks, Section: v.Section,
			Selection: v.Selection, Hidden: v.Hidden, Isolated: v.Isolated, Transparent: v.Transparent, Exploded: v.Exploded,
			Mode: v.Mode, Overlays: v.Overlays, Measurements: v.Measurements}
		out = append(out, state.check()...)
		if len(v.Annotations) > 500 {
			out = append(out, "at most 500 annotations per view")
		}
		for _, an := range v.Annotations {
			if !ValidID(KindAnnotation, an.ID) || (an.State != "active" && an.State != "deleted" && an.State != "unresolved") {
				out = append(out, "annotations need ann- ids and a state")
			}
		}
		if len(out) > 0 {
			return Invalid(out...)
		}
		return nil
	}
}
