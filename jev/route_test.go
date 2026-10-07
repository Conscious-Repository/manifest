package jev

import (
	"context"
	"os"
	"testing"

	"manifest/typesafe"
)

func routeFake(choice string, conf float64, probs map[string]float64, spec float64) *typesafe.Fake {
	return &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"intent":   typesafe.ChoiceAnswer(choice, conf, probs),
		"specific": typesafe.ScoreAnswer(spec, 0.8, nil, nil),
	}}
}

func TestRouteBands(t *testing.T) {
	cases := []struct {
		name  string
		probs map[string]float64
		conf  float64
		spec  float64
		want  string
	}{
		{"clear concrete change builds", map[string]float64{"change_app": 0.92, "talk": 0.06, "unclear": 0.02}, 0.85, 3.4, RouteBuild},
		{"clear but vague change confirms", map[string]float64{"change_app": 0.9, "talk": 0.05, "unclear": 0.05}, 0.8, 1.2, RouteConfirm},
		{"probable change confirms", map[string]float64{"change_app": 0.62, "talk": 0.3, "unclear": 0.08}, 0.55, 3.8, RouteConfirm},
		{"conversation talks", map[string]float64{"change_app": 0.04, "talk": 0.94, "unclear": 0.02}, 0.9, 0, RouteTalk},
		{"diffuse but leaning change only confirms", map[string]float64{"change_app": 0.55, "talk": 0.2, "unclear": 0.25}, 0.3, 4, RouteConfirm},
		{"diffuse talks", map[string]float64{"change_app": 0.3, "talk": 0.4, "unclear": 0.3}, 0.2, 2, RouteTalk},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := &Judge{Eval: routeFake("x", c.conf, c.probs, c.spec)}
			best := ""
			for k, v := range c.probs {
				if best == "" || v > c.probs[best] {
					best = k
				}
			}
			j.Eval = routeFake(best, c.conf, c.probs, c.spec)
			a, err := j.AdviseRoute(context.Background(), RouteInput{Message: "x"})
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != c.want || len(a.Policy) == 0 {
				t.Fatalf("route %q (%v), want %q", a.Route, a.Policy, c.want)
			}
		})
	}
}

func TestRouteSendsMessageAndContextOnly(t *testing.T) {
	f := routeFake("talk", 0.9, map[string]float64{"talk": 0.9, "change_app": 0.05, "unclear": 0.05}, 0)
	j := &Judge{Eval: f}
	if _, err := j.AdviseRoute(context.Background(), RouteInput{Message: "  can we afford the roof?  ", Recent: []string{"Olga: hi", "Liber: hello"}}); err != nil {
		t.Fatal(err)
	}
	st := f.Last().State.(map[string]any)
	if st["message"] != "can we afford the roof?" || len(st) != 2 {
		t.Fatalf("state %v", st)
	}
	if _, err := j.AdviseRoute(context.Background(), RouteInput{Message: " "}); err == nil {
		t.Fatal("empty message routed")
	}
}

// Example messages with the route a person would expect. Run live with
// MANIFEST_JEV_LIVE=1 and TYPESAFE_API_KEY set to see Jev's calibration; in CI
// the set documents intent and the bands are covered above.
var routeExamples = []struct{ msg, want string }{
	{"Make the task titles bigger", RouteBuild},
	{"Can you show the due date next to each task on the board?", RouteBuild},
	{"Change the colours to something warmer", RouteConfirm},
	{"The tasks page is confusing", RouteConfirm},
	{"What should we decide first for the roof?", RouteTalk},
	{"How much do you think the kitchen paint will cost?", RouteTalk},
	{"How do I mark a task as done?", RouteTalk},
	{"Put finished tasks in a collapsed section at the bottom of the tasks board", RouteBuild},
	{"Thanks, that's perfect", RouteTalk},
	{"Can we move the planting day to the 24th?", RouteTalk},
}

func TestRouteExamplesLive(t *testing.T) {
	if os.Getenv("MANIFEST_JEV_LIVE") == "" {
		t.Skip("live Jev calibration run: set MANIFEST_JEV_LIVE=1 and TYPESAFE_API_KEY")
	}
	c := typesafe.NewFromEnv()
	j := &Judge{Eval: c, Model: c.ModelName()}
	miss := 0
	for _, ex := range routeExamples {
		a, err := j.AdviseRoute(context.Background(), RouteInput{Message: ex.msg})
		if err != nil {
			t.Fatal(err)
		}
		mark := "ok"
		if a.Route != ex.want {
			mark, miss = "MISS", miss+1
		}
		t.Logf("%-4s %-8s want %-8s %q change_app=%.2f conf=%.2f specific=%.1f", mark, a.Route, ex.want, ex.msg, a.Intent.Probabilities["change_app"], a.Intent.Confidence, a.Specific.Score)
	}
	// A build where talk was expected is the costly miss; report, don't fail on calibration.
	t.Logf("%d of %d differ", miss, len(routeExamples))
}
