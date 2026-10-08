package construction

import (
	"testing"
)

func viewBody(asm string) map[string]any {
	return map[string]any{"name": "working view", "assemblyId": asm,
		"camera":    map[string]any{"projection": "perspective", "position": []float64{2400, 3200, 2400}, "target": []float64{1200, 400, 0}, "up": []float64{0, 0, 1}, "fov": 35, "zoom": 1},
		"bookmarks": []any{}, "section": map[string]any{"originMm": []float64{1200, 0, 0}, "normal": []float64{1, 0, 0}, "up": []float64{0, 0, 1}, "enabled": true},
		"selection": []string{}, "hidden": []string{}, "isolated": []string{}, "transparent": []string{}, "exploded": 0.25, "mode": "technical",
		"overlays":     map[string]any{"water": true, "attachment": false},
		"measurements": []any{map[string]any{"a": []float64{0, 0, 0}, "b": []float64{0, 300, 400}, "distanceMm": 999, "label": "check"}}}
}

// Views persist camera, section, visibility, mode and measurements as their
// own revisions; a view-only commit changes no assembly or report; stale
// view edits conflict; annotations anchor on components and read unresolved
// once the component is gone.
func TestConstructionViewsAndAnnotations(t *testing.T) {
	s, st, asm := templateProblem(t)
	vid := NewID(KindView)
	st2, rc, err := exec(t, s, st, "", OwnerActor(), map[string]any{"op": "SaveView", "viewId": vid, "expectedRevision": "", "view": viewBody(asm)})
	if err != nil {
		t.Fatal(err)
	}
	v := st2.Views[vid]
	if !rc.ViewOnly || st2.Problem.LatestView != vid || v.Assembly.Revision != st.Revision("assembly:"+asm) {
		t.Fatalf("view saved: viewOnly=%v latest=%s pin=%v", rc.ViewOnly, st2.Problem.LatestView, v.Assembly)
	}
	if v.Measurements[0].Distance != 500 {
		t.Fatalf("measurement recomputed from canonical points: %v", v.Measurements[0].Distance)
	}
	if st2.Revision("assembly:"+asm) != st.Revision("assembly:"+asm) || st2.Revision("validation:"+asm) != st.Revision("validation:"+asm) {
		t.Fatal("a view commit must not touch the assembly or its report")
	}
	if _, _, err := exec(t, s, st2, "", OwnerActor(), map[string]any{"op": "SaveView", "viewId": vid, "expectedRevision": "", "view": viewBody(asm)}); StatusOf(err) != 409 {
		t.Fatalf("stale view save: %v", err)
	}
	sealant := st2.Assemblies[asm].firstOf(TypeSealant).ID
	st3 := mustExec(t, s, st2, "", map[string]any{"op": "SetVisibility", "viewId": vid, "expectedRevision": st2.Revision("view:" + vid), "componentIds": []string{sealant}, "state": "hidden"})
	if len(st3.Views[vid].Hidden) != 1 {
		t.Fatal("hidden set")
	}
	ann := NewID(KindAnnotation)
	st4 := mustExec(t, s, st3, "", map[string]any{"op": "AddAnnotation", "viewId": vid, "expectedRevision": st3.Revision("view:" + vid),
		"annotation": map[string]any{"id": ann, "componentId": sealant, "point": []float64{100, 2, 330}, "text": "sealant is a maintenance item", "evidenceIds": []string{}}})
	if got := st4.Views[vid].Annotations; len(got) != 1 || got[0].Author.Kind != ActorOwner || got[0].State != "active" {
		t.Fatalf("annotation %+v", got)
	}
	if _, _, err := exec(t, s, st4, "", OwnerActor(), map[string]any{"op": "AddAnnotation", "viewId": vid, "expectedRevision": st4.Revision("view:" + vid),
		"annotation": map[string]any{"id": NewID(KindAnnotation), "componentId": NewID(KindComponent), "text": "floating", "evidenceIds": []string{}}}); StatusOf(err) != 422 {
		t.Fatalf("annotation on a non-component: %v", err)
	}
	st5 := mustExec(t, s, st4, asm, map[string]any{"op": "RemoveComponent", "componentId": sealant, "dependents": "detach"})
	res := ResolveAnnotations(st5.Views[vid], st5.Assemblies[asm])
	if res[0].State != "unresolved" {
		t.Fatalf("an annotation on a removed component reads unresolved: %+v", res[0])
	}
	bad := viewBody(asm)
	bad["exploded"] = 3
	if _, _, err := exec(t, s, st5, "", OwnerActor(), map[string]any{"op": "SaveView", "viewId": NewID(KindView), "expectedRevision": "", "view": bad}); StatusOf(err) != 422 {
		t.Fatalf("out-of-range view: %v", err)
	}
}
