package construction

import (
	"strings"
	"testing"
)

func TestDecisionPointsRaiseAnswerReopen(t *testing.T) {
	s, st, _ := templateProblem(t)
	id := NewID(KindQuestion)
	run := func(ops ...map[string]any) (*State, error) {
		next, _, err := exec(t, s, st, "", OwnerActor(), ops...)
		if err == nil {
			st = next
		}
		return next, err
	}
	if _, err := run(map[string]any{"op": "AddQuestion", "id": id, "text": "Galvanized or aluminum panels?", "why": "aluminum must not touch the brick mortar", "options": []string{"galvanized", "aluminum"}}); err != nil {
		t.Fatal(err)
	}
	q := st.Problem.Questions[0]
	if q.State != "open" || q.Stage != "approach" || q.RaisedBy != "owner" || len(q.Options) != 2 {
		t.Fatalf("raised %+v", q)
	}
	if _, err := run(map[string]any{"op": "AnswerQuestion", "id": id, "answer": "galvanized"}); err != nil {
		t.Fatal(err)
	}
	if q := st.Problem.Questions[0]; q.State != "answered" || q.Answer != "galvanized" || q.Provenance != ProvUserAssumption {
		t.Fatalf("answered %+v", q)
	}
	if _, err := run(map[string]any{"op": "SetQuestionState", "id": id, "state": "open"}); err != nil {
		t.Fatal(err)
	}
	if q := st.Problem.Questions[0]; q.State != "open" || q.Answer != "" {
		t.Fatalf("reopened %+v", q)
	}
	if _, err := run(map[string]any{"op": "AddQuestion", "id": id, "text": "again"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate id: %v", err)
	}
	if _, err := run(map[string]any{"op": "AnswerQuestion", "id": NewID(KindQuestion), "answer": "x"}); err == nil {
		t.Fatal("answered a question that does not exist")
	}
}
