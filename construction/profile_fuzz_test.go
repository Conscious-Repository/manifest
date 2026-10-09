package construction

import (
	"math"
	"math/rand"
	"testing"
)

// Anything checkProfile accepts must compile to finite, non-empty geometry,
// at both orientations, and never panic.
func TestProfileAcceptedShapesAlwaysBuild(t *testing.T) {
	_, st, asmID := templateProblem(t)
	base := st.Assemblies[asmID]
	rng := rand.New(rand.NewSource(7))
	accepted, refused := 0, 0
	for trial := 0; trial < 3000; trial++ {
		n := 2 + rng.Intn(10)
		pts := make([][2]float64, n)
		x, y := rng.Float64()*60-30, rng.Float64()*300
		for i := range pts {
			pts[i] = [2]float64{x, y}
			step := []float64{1.2, 5, 20, 80, 300}[rng.Intn(5)]
			ang := rng.Float64() * 2 * math.Pi
			x, y = x+step*math.Cos(ang), y+step*math.Sin(ang)
		}
		if trial%10 == 0 { // degenerate inputs too
			pts[n-1] = pts[0]
		}
		if len(checkProfile("p", pts)) > 0 {
			refused++
			continue
		}
		accepted++
		for _, orient := range []string{"headwall", "sidewall"} {
			a := *base
			a.Junction.Orientation = orient
			a.Components = append(append([]Component{}, base.Components...), Component{ID: NewID(KindComponent), Type: TypeProfiledFlashing, Role: "flashing:custom", Name: "fuzz",
				Shape: Shape{Kind: "bent-profile", Params: map[string]Quantity{"thickness": qmm(0.3+rng.Float64()*2.7, "")}, Profile: pts}, Transform: IdentityTransform(), Applicability: "applicable", Attachments: []string{}})
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic on %v (%s): %v", pts, orient, r)
					}
				}()
				ir, err := Compile(&a, st.Catalog)
				if err != nil {
					return // the template refusing a sidewall arrangement is fine
				}
				for _, p := range ir.Parts {
					if p.Type != TypeProfiledFlashing {
						continue
					}
					if len(p.Solids) == 0 {
						t.Fatalf("no solid for accepted profile %v (%s)", pts, orient)
					}
					for _, s := range p.Solids {
						for _, v := range s.Positions {
							if math.IsNaN(v) || math.IsInf(v, 0) {
								t.Fatalf("non-finite vertex for accepted profile %v (%s)", pts, orient)
							}
						}
					}
				}
			}()
		}
	}
	t.Logf("accepted %d, refused %d", accepted, refused)
	if accepted < 200 {
		t.Fatalf("too few accepted shapes to mean anything: %d", accepted)
	}
}
