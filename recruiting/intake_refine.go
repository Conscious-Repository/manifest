package recruiting

import (
	"strings"

	"manifest/recruiting/sources"
)

// RUNGS 3–5 OF THE CASCADE — what a fetch found out, applied.
//
// Everything here is pure: it is handed what a page or an API said and
// decides what that means for our six classes. The fetching lives in
// recruiting/sources (typed_fetch.go, sources_github.go); the meaning lives
// here, beside the classes it names, so the table can be pinned exhaustively
// without a network.
//
// THE RULE THAT KEEPS THIS HONEST: a fetched rung may only speak where the
// pure rungs left a question open — the class is empty, or the resolution
// carried alternatives, which is how rungs 1 and 2 say "I am not sure". A
// page can never talk the resolver out of a DOI. `Certain()` is the test the
// UI uses for the same reason.

// schemaClass maps a schema.org @type onto one of our six classes. It is
// deliberately incomplete: a type not in the table leaves the question open,
// which is a better answer than a confident wrong one.
var schemaClass = map[string]string{
	// people
	"person": SeedPerson,
	"author": SeedPerson,
	// the research organisations — a lab in our sense is anywhere the work
	// is done rather than sold
	"collegeoruniversity":     SeedLab,
	"educationalorganization": SeedLab,
	"researchorganization":    SeedLab,
	"researchproject":         SeedLab,
	"medicalorganization":     SeedLab,
	"hospital":                SeedLab,
	"governmentorganization":  SeedLab,
	"ngo":                     SeedLab,
	"library":                 SeedLab,
	// the commercial ones
	"corporation":    SeedCompany,
	"localbusiness":  SeedCompany,
	"onlinebusiness": SeedCompany,
	"store":          SeedCompany,
	// works
	"scholarlyarticle": SeedWork,
	"article":          SeedWork,
	"newsarticle":      SeedWork,
	"report":           SeedWork,
	"dataset":          SeedWork,
	"thesis":           SeedWork,
	"publication":      SeedWork,
	"creativework":     SeedWork,
	// code
	"softwaresourcecode":  SeedRepo,
	"softwareapplication": SeedRepo,
	// media
	"blog":           SeedMedia,
	"blogposting":    SeedMedia,
	"podcastseries":  SeedMedia,
	"podcastepisode": SeedMedia,
	"radioseries":    SeedMedia,
	"tvseries":       SeedMedia,
	"videoobject":    SeedMedia,
	"audioobject":    SeedMedia,
	"periodical":     SeedMedia,
}

// schemaAmbiguous are the types that narrow the question without answering
// it. A bare `Organization` rules out person, work, repo and media — that is
// worth saying, and worth NOT pretending is an answer.
var schemaAmbiguous = map[string][]string{
	"organization":     {SeedLab, SeedCompany},
	"place":            {SeedLab, SeedCompany},
	"webpage":          nil,
	"website":          nil,
	"collection":       nil,
	"itemlist":         nil,
	"breadcrumb":       nil,
	"listitem":         nil,
	"organizationrole": nil,
}

// ogClass maps og:type. It is a tiebreaker and nothing more: on most of the
// web og:type is `website`, which means the page has a URL.
var ogClass = map[string]string{
	"profile":       SeedPerson,
	"article":       SeedWork,
	"book":          SeedWork,
	"music.song":    SeedMedia,
	"music.album":   SeedMedia,
	"video.episode": SeedMedia,
	"video.movie":   SeedMedia,
	"video.tv_show": SeedMedia,
}

// RefineWithPage applies rung 3 (the page's own JSON-LD @type) and, only
// when JSON-LD said nothing, rung 5 (og:type). It returns r unchanged when
// the resolution was already certain or the page declared nothing usable.
func RefineWithPage(r Resolution, p sources.PageTypes) Resolution {
	if r.Certain() || p.Empty() {
		return r
	}
	// rung 3 — the first @type the page led with that we have a meaning for
	for _, t := range p.JSONLD {
		key := schemaKey(t)
		if class, ok := schemaClass[key]; ok {
			return applyClass(r, class, RungPage, t,
				"the page says "+t)
		}
		if alts, ok := schemaAmbiguous[key]; ok {
			if len(alts) == 0 {
				continue // WebPage/ItemList: says nothing, keep reading
			}
			r.Rung, r.Asked = RungPage, t
			r.Class = ""
			r.Suggest = alts
			r.Why = "the page says " + t + " — a lab or a company?"
			return r
		}
	}
	// rung 5 — og:type, and only as a tiebreaker
	if class, ok := ogClass[strings.ToLower(strings.TrimSpace(p.OGType))]; ok {
		return applyClass(r, class, RungOpenGraph, p.OGType,
			"the page's og:type is "+p.OGType)
	}
	return r
}

// schemaKey normalizes one @type token. A page may write `Organization`,
// `schema:Organization` or `https://schema.org/Organization` and mean the
// same thing — the meaning layer normalizes for itself rather than trusting
// whoever handed it the token to have done it.
func schemaKey(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndexAny(t, "/#:"); i >= 0 {
		t = t[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(t))
}

// RefineWithAccount applies rung 4: GitHub's own answer to what
// github.com/<login> is. Nothing else may use it — an account type is only
// meaningful for the account host that issued it.
func RefineWithAccount(r Resolution, accountType string) Resolution {
	if r.Kind != "github-user" {
		return r
	}
	switch strings.ToLower(strings.TrimSpace(accountType)) {
	case "user":
		r.Rung, r.Asked = RungAccount, accountType
		r.Class, r.Dest = SeedPerson, DestCandidate
		r.Suggest = nil
		r.Why = "GitHub says this account is a person"
	case "organization":
		r.Rung, r.Asked = RungAccount, accountType
		r.Class, r.Dest = SeedCompany, DestSeed
		r.Suggest = []string{SeedLab}
		r.Provisional = true
		r.Why = "GitHub says this account is an organisation, not a person"
	}
	return r
}

// applyClass writes one rung's answer onto a resolution, moving the
// destination with it — the class is what decides where a commit lands, and
// the two disagreeing is how a company ends up on the board.
func applyClass(r Resolution, class, rung, asked, why string) Resolution {
	r.Class = class
	r.Dest = DestForClass(class)
	r.Rung, r.Asked, r.Why = rung, asked, why
	// what the pure rung offered as alternatives is now the fallback, minus
	// the answer itself
	r.Suggest = withoutClass(r.Suggest, class)
	return r
}

// DestForClass is the one place the class→destination rule lives. A person is
// someone we are deciding about; everything else is a thing we sweep from.
func DestForClass(class string) string {
	if class == SeedPerson {
		return DestCandidate
	}
	return DestSeed
}

func withoutClass(list []string, class string) []string {
	var out []string
	for _, s := range list {
		if s != class {
			out = append(out, s)
		}
	}
	return out
}
