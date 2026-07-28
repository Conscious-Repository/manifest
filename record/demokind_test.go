// Demo-kind registration test (kernel-extraction Pass A §A2): proves a NEW
// domain — shaped like the upcoming Aion program/experiment records — can be
// declared entirely against the kernel's public API. The whole "domain" below
// is what a kind registration is: a section grammar, a depth policy, two
// declared fields, a slug identity, one sidecar, an archive policy. If this
// test ever needs a kernel edit to pass, the kernel has failed its standing
// metric ("adding the Aion domain must be a declaration + views").
package record_test

import (
	"strings"
	"testing"

	"manifest/record"
)

// ---- the declaration (everything a kind IS) ----

type protoItem struct {
	Text     string
	Checked  bool
	Dose     string // [dose:: …]
	When     string // [when:: …]
	Children []*protoItem
}

type program struct {
	Title string
	Proto []*protoItem // "## protocol" outline (relative nesting, kernel stack)
	Extra []string     // any other lines, preserved verbatim
}

func parseProgram(content string) *program {
	p := &program{}
	_, body, _ := record.SplitFrontmatter(content)
	inProto := false
	var frames record.Frames[*protoItem]
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "# ") && p.Title == "":
			p.Title = strings.TrimPrefix(line, "# ")
		case strings.HasPrefix(line, "## "):
			inProto = strings.TrimSpace(line) == "## protocol"
			frames.Reset()
			if !inProto {
				p.Extra = append(p.Extra, line)
			}
		case inProto && record.CheckboxRe.MatchString(line):
			m := record.CheckboxRe.FindStringSubmatch(line)
			text, fields := record.ParseFields(m[3])
			it := &protoItem{Text: text, Checked: m[2] == "x" || m[2] == "X"}
			for _, f := range fields {
				switch strings.ToLower(f.Key) {
				case "dose":
					it.Dose = f.Value
				case "when":
					it.When = f.Value
				}
			}
			depth, parent := frames.Descend(record.IndentWidth(m[1]))
			if depth == 0 {
				p.Proto = append(p.Proto, it)
			} else {
				parent.Children = append(parent.Children, it)
			}
			frames.Push(record.IndentWidth(m[1]), it)
		case strings.TrimSpace(line) == "":
		default:
			p.Extra = append(p.Extra, line)
		}
	}
	return p
}

func emitProgram(p *program) string {
	var out []string
	out = append(out, "# "+p.Title, "", "## protocol")
	var walk func(items []*protoItem, depth int)
	walk = func(items []*protoItem, depth int) {
		for _, it := range items {
			mark := " "
			if it.Checked {
				mark = "x"
			}
			line := strings.Repeat(record.IndentUnit, depth) + "- [" + mark + "] " + it.Text
			if it.Dose != "" {
				line += " " + record.EmitField("dose", it.Dose)
			}
			if it.When != "" {
				line += " " + record.EmitField("when", it.When)
			}
			out = append(out, line)
			walk(it.Children, depth+1)
		}
	}
	walk(p.Proto, 0)
	if len(p.Extra) > 0 {
		out = append(out, "")
		out = append(out, p.Extra...)
	}
	return "---\ntype: program\n---\n\n" + strings.Join(out, "\n") + "\n"
}

// ---- the registration proof ----

const demoDoc = `---
type: program
---

# Creatine loading

## protocol
- [ ] morning dose [dose:: 5g] [when:: am]
    - [x] with food [when:: pm]
- [x] baseline bloodwork

## log
2026-07-20 started
`

func TestDemoKindRegistration(t *testing.T) {
	// fixpoint: parse → emit is byte-identical on the canonical form
	p := parseProgram(demoDoc)
	if got := emitProgram(p); got != demoDoc {
		t.Fatalf("round-trip diverged:\n--- got ---\n%s\n--- want ---\n%s", got, demoDoc)
	}

	// the declared fields and depth policy actually parsed
	if len(p.Proto) != 2 || p.Proto[0].Dose != "5g" || p.Proto[0].When != "am" {
		t.Fatalf("proto: %+v", p.Proto)
	}
	if len(p.Proto[0].Children) != 1 || !p.Proto[0].Children[0].Checked {
		t.Fatalf("nesting: %+v", p.Proto[0].Children)
	}
	if len(p.Extra) != 2 || p.Extra[0] != "## log" {
		t.Fatalf("extra preserved: %q", p.Extra)
	}

	// hand-edit readback: mutate, emit, re-parse — still a fixpoint
	p.Proto[0].Checked = true
	edited := emitProgram(p)
	if emitProgram(parseProgram(edited)) != edited {
		t.Fatal("edited doc is not a fixpoint")
	}

	// identity + sidecar + zone come from the kernel, by declaration
	slug := record.Slug(p.Title, 48)
	if slug != "creatine-loading" {
		t.Fatalf("slug: %q", slug)
	}
	rel := "system/aion/" + slug + ".md"
	if record.Sidecar(rel, record.SidecarLedger) != "system/aion/creatine-loading.ledger.csv" {
		t.Fatalf("sidecar: %q", record.Sidecar(rel, record.SidecarLedger))
	}
	if record.ZoneOf(rel, "system", "extrinsic") != record.ZoneSystem {
		t.Fatal("zone")
	}

	// archive policy: append-only dated sections, kernel-owned
	arch := record.AppendSection("", "# aion — archive", "2026-07",
		[]string{"- baseline bloodwork [closed:: 2026-07-28]"})
	arch2 := record.AppendSection(arch, "# aion — archive", "2026-07",
		[]string{"- second entry"})
	if !strings.Contains(arch2, "## 2026-07\n- baseline bloodwork") ||
		strings.Count(arch2, "## 2026-07") != 1 {
		t.Fatalf("archive:\n%s", arch2)
	}
}
