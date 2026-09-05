package tasks

import (
	"strings"
	"testing"
)

// artifact binding (P1 artifacts): [outputs::] / [inputs::] round-trip as
// id-list fields by reference; a file without them is byte-untouched.

const bindingSample = `# To Do

## Aion
- [ ] research the UAE market [todo:: aion/uae-research] [added:: 2026-08-10] [outputs:: 3f9a1c2b4d5e6f70]
- [ ] write the UAE memo [added:: 2026-08-12] [depends:: aion/uae-research] [outputs:: a0b1c2d3e4f50617] [inputs:: 3f9a1c2b4d5e6f70, 1111222233334444]
- [x] old brief [added:: 2026-07-01] [done:: 2026-07-20] [outputs:: 9999888877776666] [est:: 3]
`

func TestBindingFixpoint(t *testing.T) {
	out := Serialize(Parse(bindingSample))
	if out != bindingSample {
		t.Fatalf("canonical binding sample changed:\n%s", out)
	}
	if Serialize(Parse(out)) != out {
		t.Fatal("binding sample not a fixpoint")
	}
	for _, s := range []string{sample, sampleV2, coordSample} {
		if Serialize(Parse(s)) != s {
			t.Fatal("a legacy sample changed once artifact fields exist")
		}
	}
}

func TestBindingParseAndMutate(t *testing.T) {
	d := Parse(bindingSample)
	dom := d.Domain("Aion")
	research, memo, old := dom.Tasks[0], dom.Tasks[1], dom.Tasks[2]
	if strings.Join(research.Outputs, "|") != "3f9a1c2b4d5e6f70" || research.Inputs != nil {
		t.Fatalf("research: %+v", research)
	}
	if strings.Join(memo.Inputs, "|") != "3f9a1c2b4d5e6f70|1111222233334444" || strings.Join(memo.Outputs, "|") != "a0b1c2d3e4f50617" {
		t.Fatalf("memo: %+v", memo)
	}
	// the keys never leak into passthrough; an unknown field beside them survives
	for _, tk := range []*Task{research, memo, old} {
		if tk.FieldValue("outputs") != "" || tk.FieldValue("inputs") != "" {
			t.Fatalf("binding key leaked into Fields: %+v", tk.Fields)
		}
	}
	if old.FieldValue("est") != "3" {
		t.Fatalf("passthrough lost: %+v", old.Fields)
	}
	// the view carries the ids by reference
	v := d.View(now)
	if tv := v.Domains[0].Tasks[1]; strings.Join(tv.Inputs, "|") != "3f9a1c2b4d5e6f70|1111222233334444" || tv.Outputs[0] != "a0b1c2d3e4f50617" {
		t.Fatalf("view: %+v", tv)
	}

	// mutate: add / remove / set, deduped, then a byte-stable serialize
	memo.AddInput("1111222233334444") // present: no-op
	memo.AddInput(" 5555666677778888 ")
	memo.RemoveInput("3f9a1c2b4d5e6f70")
	research.AddOutput("")
	research.SetOutputs([]string{"3f9a1c2b4d5e6f70", "3f9a1c2b4d5e6f70", "abcdefabcdefabcd"})
	old.RemoveOutput("9999888877776666")
	out := Serialize(d)
	for _, want := range []string{
		"[outputs:: a0b1c2d3e4f50617] [inputs:: 1111222233334444, 5555666677778888]",
		"[outputs:: 3f9a1c2b4d5e6f70, abcdefabcdefabcd]",
		"- [x] old brief [added:: 2026-07-01] [done:: 2026-07-20] [est:: 3]\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if Serialize(Parse(out)) != out {
		t.Fatal("mutated doc not a fixpoint")
	}
	// two fields with the same key merge on parse
	tk := ParseLine(false, "x [inputs:: a] [inputs:: b, a]")
	if strings.Join(tk.Inputs, "|") != "a|b" {
		t.Fatalf("merge: %v", tk.Inputs)
	}
}
