package recruiting

import (
	"regexp"
	"strings"
)

// ---- the uniform name of a place (2026-09-21) ----
//
// A lab's own name comes in every shape a web page can give it — "Jianmin
// Cui Lab", "Chen Ultrasound Laboratory", "Huebsch Lab — Biomaterials and
// Tissue Engineering", "CIMED — Center for Investigation of Membrane
// Excitability Diseases". The owner catalogues people BY LAB, so every
// surface names a place the same way: the principal's surname + "Lab", or a
// centre's short name, then the institution's short tag — "Cui Lab · WashU",
// "CIMED · WashU". A `[label:: …]` on the seed row overrides the rule when
// the derivation is wrong for a place; the full name stays on the row and
// in the tooltip.

var (
	labSuffixRe     = regexp.MustCompile(`(?i)^(.*?)\s+(laboratory|laboratories|labs?|research group|group)$`)
	labDescriptorRe = regexp.MustCompile(`\s+(—|–|-|:|\|)\s+`)
	labSpaceRe      = regexp.MustCompile(`\s+`)
)

// fieldSuffixes mark a word as a discipline, not a person: "Ultrasound",
// "Bioelectronics", "Biomaterials", "Imaging" — so "Chen Ultrasound Lab" is
// Chen's, while "Jianmin Cui Lab" is Cui's (a given name precedes a surname).
var fieldSuffixes = []string{"ics", "ology", "ogy", "sound", "ing", "istry", "ence", "ials", "ation", "omics", "ular", "graphy", "metry", "onics", "ical", "ology", "iology", "ience"}

var fieldWords = map[string]bool{
	"neuro": true, "cardiac": true, "tissue": true, "cell": true, "cells": true, "systems": true, "synthetic": true,
	"computational": true, "quantum": true, "materials": true, "mechanics": true, "optics": true, "photonics": true,
	"vision": true, "brain": true, "cancer": true, "stem": true, "protein": true, "gene": true, "rna": true, "dna": true,
	"micro": true, "nano": true, "bio": true, "medical": true, "clinical": true, "translational": true, "molecular": true,
}

func isFieldWord(w string) bool {
	l := strings.ToLower(strings.Trim(w, "()[],.&"))
	if fieldWords[l] {
		return true
	}
	for _, s := range fieldSuffixes {
		if len(l) > len(s)+1 && strings.HasSuffix(l, s) {
			return true
		}
	}
	return false
}

// institutionTags: the short tag an org reads as. Matched case-insensitively
// against the org line; the first hit wins, else the org's first segment.
var institutionTags = [][2]string{
	{"washington university in st. louis", "WashU"}, {"washington university", "WashU"}, {"wustl", "WashU"}, {"washu", "WashU"},
	{"massachusetts institute of technology", "MIT"}, {"mit media lab", "MIT"}, {"mit ", "MIT"}, {"mit,", "MIT"}, {"stanford", "Stanford"}, {"harvard", "Harvard"},
	{"university of california, san francisco", "UCSF"}, {"ucsf", "UCSF"}, {"university of california, berkeley", "Berkeley"},
	{"caltech", "Caltech"}, {"california institute of technology", "Caltech"}, {"johns hopkins", "JHU"},
	{"university of pennsylvania", "Penn"}, {"columbia", "Columbia"}, {"yale", "Yale"}, {"princeton", "Princeton"},
	{"university of michigan", "Michigan"}, {"university of washington", "UW"}, {"eth zürich", "ETH"}, {"eth zurich", "ETH"},
	{"imperial college", "Imperial"}, {"oxford", "Oxford"}, {"cambridge", "Cambridge"}, {"duke", "Duke"},
	{"cornell", "Cornell"}, {"nyu", "NYU"}, {"new york university", "NYU"}, {"ucla", "UCLA"}, {"ucsd", "UCSD"},
	{"georgia tech", "Georgia Tech"}, {"georgia institute of technology", "Georgia Tech"}, {"carnegie mellon", "CMU"},
	{"northwestern", "Northwestern"}, {"university of chicago", "UChicago"}, {"rice university", "Rice"},
	{"broad institute", "Broad"}, {"scripps", "Scripps"}, {"salk", "Salk"}, {"janelia", "Janelia"},
}

// OrgTag is the institution's short tag for an org line: "Washington
// University in St. Louis, Biomedical Engineering" → "WashU".
func OrgTag(org string) string {
	l := strings.ToLower(org)
	for _, t := range institutionTags {
		if strings.Contains(l, t[0]) {
			return t[1]
		}
	}
	head := strings.TrimSpace(strings.SplitN(org, ",", 2)[0])
	if len(head) > 28 {
		head = strings.TrimSpace(head[:28])
	}
	return head
}

// labHead reduces a lab's name to its short form without the institution:
// "Jianmin Cui Lab" → "Cui Lab"; "Chen Ultrasound Laboratory" → "Chen Lab";
// "Huebsch Lab — Biomaterials …" → "Huebsch Lab"; "CIMED — Center for …" →
// "CIMED"; "WashU Center for Engineering MechanoBiology" → "Center for
// Engineering MechanoBiology" (the tag says WashU).
func labHead(name, tag string) string {
	head := strings.TrimSpace(name)
	if parts := labDescriptorRe.Split(head, 2); len(parts) > 1 && strings.TrimSpace(parts[0]) != "" {
		head = strings.TrimSpace(parts[0])
	}
	head = labSpaceRe.ReplaceAllString(head, " ")
	if m := labSuffixRe.FindStringSubmatch(head); m != nil {
		words := strings.Fields(m[1])
		var people []string
		for _, w := range words {
			if !isFieldWord(w) {
				people = append(people, w)
			}
		}
		switch {
		case len(words) == 0 || len(people) == 0:
			return head // "Synthetic Neurobiology Group": a field, not a person — keep it whole
		case !isFieldWord(words[len(words)-1]):
			return words[len(words)-1] + " Lab" // "Jianmin Cui Lab": the surname closes the name
		default:
			return people[0] + " Lab" // "Chen Ultrasound Lab": the person leads, the field follows
		}
	}
	// a centre or institute: drop a leading institution tag, keep the rest
	if tag != "" {
		for _, p := range []string{tag + " ", strings.ToLower(tag) + " "} {
			if strings.HasPrefix(strings.ToLower(head), strings.ToLower(p)) {
				head = strings.TrimSpace(head[len(p):])
				break
			}
		}
	}
	return head
}

// LabLabel is the uniform display name of a seed: the override when set,
// else for a lab "<head> · <tag>", else the name's head as it is.
func LabLabel(s Seed) string {
	if l := strings.TrimSpace(s.Label); l != "" {
		return l
	}
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return s.ID
	}
	if s.Class != SeedLab {
		return name
	}
	tag := OrgTag(s.Org)
	head := labHead(name, tag)
	if head == "" {
		head = name
	}
	if tag != "" && !strings.Contains(strings.ToLower(head), strings.ToLower(tag)) {
		return head + " · " + tag
	}
	return head
}
