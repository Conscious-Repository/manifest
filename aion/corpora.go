package aion

// Files are the seven corpus files under <vault>/system/aion/.
var Files = []string{
	"people.md", "vto.md", "backlog.md", "heuristics.md",
	"hiring.md", "references.md", "finances.md",
}

// Corpora is the filename → parse∘serialize round-trip registry: the hook
// the kernel's central property tests (record/corpus_test.go) and the
// domain's own fixpoint tests drive. Byte-identical output on canonical
// input is the fixpoint guarantee (ARCHITECTURE §3).
var Corpora = map[string]func(raw string) string{
	"people.md":     func(raw string) string { return SerializePeople(ParsePeople(raw)) },
	"vto.md":        func(raw string) string { return SerializeVTO(ParseVTO(raw)) },
	"backlog.md":    func(raw string) string { return SerializeBacklog(ParseBacklog(raw)) },
	"heuristics.md": func(raw string) string { return SerializeHeuristics(ParseHeuristics(raw)) },
	"hiring.md":     func(raw string) string { return SerializeHiring(ParseHiring(raw)) },
	"references.md": func(raw string) string { return SerializeReferences(ParseReferences(raw)) },
	"finances.md":   func(raw string) string { return SerializeFinances(ParseFinances(raw)) },
}
