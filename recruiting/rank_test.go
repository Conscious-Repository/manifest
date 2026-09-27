package recruiting

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

func pubRow(url, snippet string) sources.Evidence {
	return sources.Evidence{SourceID: "fake", URLOrFile: url, Kind: sources.EvidencePublication, Snippet: snippet, Trust: sources.TrustHigh}
}

// Both adapters' labelled snippets read back into the same authorship.
func TestAuthorshipsReadBothAdapterSpellings(t *testing.T) {
	d := sources.CandidateDraft{Evidence: []sources.Evidence{
		pubRow("https://pubmed.ncbi.nlm.nih.gov/1/", "author: Dana Reyes · position: last of 7 · title: coils · pubdate: 2024 Mar · pmid: 1"),
		pubRow("https://openalex.org/W2", "author: Dana Reyes · position: first author · title: x · pubdate: 2021-05-01 · cited_by_count: 40 · authors: 1"),
		pubRow("https://openalex.org/A9", "works matching this search attributed to this author: 12 (group_by authorships.author.id · q)"),
		pubRow("https://pubmed.ncbi.nlm.nih.gov/1/", "a second row citing the same paper"),
	}}
	ws, matching := sources.Authorships(d)
	if matching != 12 || len(ws) != 2 {
		t.Fatalf("matching=%d works=%+v", matching, ws)
	}
	if ws[0].Position != "last" || ws[0].Authors != 7 || ws[0].Year != 2024 {
		t.Fatalf("pubmed row: %+v", ws[0])
	}
	if ws[1].Position != "sole" || ws[1].Cited != 40 || ws[1].Year != 2021 {
		t.Fatalf("openalex row: %+v", ws[1])
	}
}

func worksDraft(n int, authors int, pos string, year int) sources.CandidateDraft {
	var d sources.CandidateDraft
	for i := 0; i < n; i++ {
		d.Evidence = append(d.Evidence, pubRow("https://example.test/w/"+strconv.Itoa(i),
			"position: "+pos+" of "+strconv.Itoa(authors)+" · pubdate: "+strconv.Itoa(year)))
	}
	return d
}

// ⚠ The rank is monotonic in relevant works, a 2-author paper outranks a
// 40-author one, and every number it used is in the reasons.
func TestRankIsMonotonicTransparentAndDiscountsConsortia(t *testing.T) {
	prev := -1.0
	for n := 0; n <= 6; n++ {
		r := RankDraft(worksDraft(n, 4, "middle", 2024), 2026)
		if r.Score <= prev && n > 0 {
			t.Fatalf("adding a matching work did not raise the score: %d works → %v (was %v)", n, r.Score, prev)
		}
		prev = r.Score
	}
	small := RankDraft(worksDraft(1, 2, "last", 2025), 2026)
	big := RankDraft(worksDraft(1, 40, "middle", 2025), 2026)
	if small.Score <= big.Score {
		t.Fatalf("a 2-author last-author paper should outrank a 40-author byline: %v vs %v", small, big)
	}
	joined := strings.Join(small.Reasons, " | ")
	if !strings.Contains(joined, "1 matching work") || !strings.Contains(joined, "latest 2025") || !strings.Contains(joined, "last author ×1") {
		t.Fatalf("reasons: %q", joined)
	}
	if !strings.Contains(strings.Join(big.Reasons, " | "), "consortium paper counted at ¼") {
		t.Fatalf("the discount was not said: %v", big.Reasons)
	}
	amb := worksDraft(3, 4, "last", 2025)
	clear := RankDraft(amb, 2026)
	amb.Note = "identity: ambiguous — initials only"
	if got := RankDraft(amb, 2026); got.Score*2 != clear.Score {
		t.Fatalf("ambiguity should halve: %v vs %v", got.Score, clear.Score)
	}
	// deterministic
	if RankDraft(worksDraft(3, 5, "first", 2020), 2026).Score != RankDraft(worksDraft(3, 5, "first", 2020), 2026).Score {
		t.Fatal("rank is not deterministic")
	}
}

// ⚠ MERGE ONLY ON A DURABLE IDENTIFIER: the same ORCID across two runs is one
// row with both evidence sets; a namesake with no shared id is two rows that
// each SAY they share a name. The rank never reaches the cache.
func TestMergedPeopleJoinsOnDurableIdsOnlyAndNeverStoresRank(t *testing.T) {
	a1 := citedDraft("Ada Coil", "A1")
	a1.Links = append(a1.Links, "https://orcid.org/0000-0001-2345-6789")
	a2 := citedDraft("Ada Coil", "A2")
	a2.Links = append(a2.Links, "https://orcid.org/0000-0001-2345-6789")
	namesake := citedDraft("Ada Coil", "A3")
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{a1, namesake}})
	run1, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "coils", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	rs.Register(&fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{a2}})
	if _, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "rf", DryRun: true}, testNow); err != nil {
		t.Fatal(err)
	}
	people := rs.MergedPeople(testNow)
	var ada, other *MergedPerson
	for i := range people {
		switch len(people[i].Members) {
		case 2:
			ada = &people[i]
		case 1:
			other = &people[i]
		}
	}
	if ada == nil || other == nil || len(people) != 2 {
		t.Fatalf("want one merged Ada (2 runs) and one separate namesake: %+v", people)
	}
	if ada.Rank.Works != 2 || !ada.Namesake || !other.Namesake {
		t.Fatalf("merged works or namesake flag: ada=%+v other=%+v", ada, other)
	}
	raw, err := os.ReadFile(filepath.Join(rs.Root(), run1.ID, "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"rank"`) {
		t.Fatal("a derived rank was written to the run cache")
	}
	listed := rs.Runs(testNow)
	if len(listed) == 0 || listed[0].Drafts[0].Rank == nil {
		t.Fatal("the rank is not projected on read")
	}
}
