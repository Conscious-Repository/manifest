package fundraising

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SharedOpportunity is the deliberately flattened fundraising projection used
// by Google Sheets. It contains no contact emails, note paths, vault paths, or
// import metadata.
type SharedOpportunity struct {
	ID                     string   `json:"id"`
	Firm                   string   `json:"firm"`
	Website                string   `json:"website,omitempty"`
	People                 []string `json:"people"`
	Source                 string   `json:"source,omitempty"`
	Status                 string   `json:"status"`
	Interest               string   `json:"interest"`
	Amount                 float64  `json:"amount,omitempty"`
	Currency               string   `json:"currency"`
	LastTouchpoint         string   `json:"lastTouchpoint,omitempty"`
	LastTouchpointDate     string   `json:"lastTouchpointDate,omitempty"`
	ComputedLastTouchpoint string   `json:"computedLastTouchpoint,omitempty"`
	NextStep               string   `json:"nextStep,omitempty"`
	NextStepDue            string   `json:"nextStepDue,omitempty"`
	Notes                  string   `json:"notes,omitempty"`
	Archived               bool     `json:"archived"`
}

func SharedFromOpportunity(op Opportunity) SharedOpportunity {
	people := make([]string, 0, len(op.People)+len(op.UnlinkedPeople))
	for _, p := range op.People {
		if name := strings.TrimSpace(p.Display); name != "" {
			people = append(people, name)
		}
	}
	people = append(people, op.UnlinkedPeople...)
	people = normalizePlainPeople(people)
	sort.SliceStable(people, func(i, j int) bool { return strings.ToLower(people[i]) < strings.ToLower(people[j]) })
	source := ""
	if op.Source != nil {
		if op.Source.Contact != nil {
			source = strings.TrimSpace(op.Source.Contact.Display)
		} else {
			source = strings.TrimSpace(op.Source.Text)
		}
	}
	return SharedOpportunity{
		ID: op.ID, Firm: op.Firm, Website: op.Website, People: people, Source: source,
		Status: op.Status, Interest: op.Interest, Amount: op.Amount, Currency: op.Currency,
		LastTouchpoint: op.LastTouchpoint, LastTouchpointDate: op.LastTouchpointDate,
		ComputedLastTouchpoint: op.ComputedLastTouchpoint, NextStep: op.NextStep,
		NextStepDue: op.NextStepDue, Notes: op.Notes, Archived: op.Archived,
	}
}

var sharedEditableFields = []string{
	"firm", "website", "people", "source", "status", "interest", "amount", "currency",
	"lastTouchpoint", "lastTouchpointDate", "nextStep", "nextStepDue", "notes",
}

func sharedFieldMap(op SharedOpportunity) map[string]string {
	people := normalizePlainPeople(op.People)
	sort.SliceStable(people, func(i, j int) bool { return strings.ToLower(people[i]) < strings.ToLower(people[j]) })
	return map[string]string{
		"firm": strings.TrimSpace(op.Firm), "website": strings.TrimSpace(op.Website),
		"people": strings.Join(people, "; "), "source": strings.TrimSpace(op.Source),
		"status": strings.ToLower(strings.TrimSpace(op.Status)), "interest": strings.ToLower(strings.TrimSpace(op.Interest)),
		"amount": strconv.FormatFloat(op.Amount, 'f', -1, 64), "currency": strings.ToUpper(strings.TrimSpace(op.Currency)),
		"lastTouchpoint": strings.TrimSpace(op.LastTouchpoint), "lastTouchpointDate": strings.TrimSpace(op.LastTouchpointDate),
		"nextStep": strings.TrimSpace(op.NextStep), "nextStepDue": strings.TrimSpace(op.NextStepDue), "notes": strings.TrimSpace(op.Notes),
	}
}

func sharedWithFields(base SharedOpportunity, fields map[string]string) (SharedOpportunity, error) {
	op := base
	for key, raw := range fields {
		switch key {
		case "firm":
			op.Firm = strings.TrimSpace(raw)
			if op.Firm == "" {
				return op, errors.New("firm is required")
			}
		case "website":
			op.Website = strings.TrimSpace(raw)
		case "people":
			op.People = splitPeople(raw)
		case "source":
			op.Source = strings.TrimSpace(raw)
		case "status":
			op.Status = strings.ToLower(strings.TrimSpace(raw))
			if !validStatus(op.Status) {
				return op, errors.New("invalid status")
			}
		case "interest":
			op.Interest = strings.ToLower(strings.TrimSpace(raw))
			if !validInterest(op.Interest) {
				return op, errors.New("invalid interest")
			}
		case "amount":
			if strings.TrimSpace(raw) == "" {
				op.Amount = 0
				break
			}
			amount, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil || amount < 0 {
				return op, errors.New("amount must be a positive number")
			}
			op.Amount = amount
		case "currency":
			op.Currency = strings.ToUpper(strings.TrimSpace(raw))
			if len(op.Currency) != 3 {
				return op, errors.New("currency must be a three-letter code")
			}
		case "lastTouchpoint":
			op.LastTouchpoint = strings.TrimSpace(raw)
		case "lastTouchpointDate":
			op.LastTouchpointDate = strings.TrimSpace(raw)
			if err := validISODate(op.LastTouchpointDate); err != nil {
				return op, fmt.Errorf("last touchpoint date: %w", err)
			}
		case "nextStep":
			op.NextStep = strings.TrimSpace(raw)
		case "nextStepDue":
			op.NextStepDue = strings.TrimSpace(raw)
			if err := validISODate(op.NextStepDue); err != nil {
				return op, fmt.Errorf("next step due: %w", err)
			}
		case "notes":
			op.Notes = strings.TrimSpace(raw)
		}
	}
	return op, nil
}

func validISODate(v string) error {
	if v == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return errors.New("must be YYYY-MM-DD")
	}
	return nil
}

func splitPeople(raw string) []string {
	return normalizePlainPeople(strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '\n' }))
}

// SharedUpdate converts a flattened collaborator edit into one atomic Store
// update. Existing exact contacts remain linked; unknown names remain local
// plaintext. Editing a linked source turns it into plaintext.
func (s *Store) SharedUpdate(id string, desired SharedOpportunity, fields []string) (Opportunity, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, ok := s.Get(id)
	if !ok {
		return Opportunity{}, fmt.Errorf("opportunity %q not found", id)
	}
	want := map[string]bool{}
	for _, field := range fields {
		want[field] = true
	}
	set := map[string]any{}
	values := sharedFieldMap(desired)
	for _, field := range sharedEditableFields {
		if want[field] && field != "people" && field != "source" {
			set[field] = values[field]
		}
	}
	if want["people"] {
		linked, plain := s.resolveSharedPeople(desired.People, current)
		set["people"] = linked
		set["unlinkedPeople"] = plain
	}
	if want["source"] {
		value := strings.TrimSpace(desired.Source)
		if value == "" {
			set["source"] = nil
		} else if current.Source != nil && current.Source.Contact != nil && strings.EqualFold(value, strings.TrimSpace(current.Source.Contact.Display)) {
			// The visible Sheet value did not change semantically; retain identity.
			set["source"] = current.Source
		} else {
			set["source"] = value
		}
	}
	if len(set) == 0 {
		return current, nil
	}
	return s.update(id, set)
}

// CreateShared validates and writes a collaborator-created opportunity in one
// serialized operation so an invalid row cannot leave a partial Markdown file.
func (s *Store) CreateShared(desired SharedOpportunity) (Opportunity, error) {
	defaults := SharedOpportunity{Firm: desired.Firm, Status: StatusProspect, Interest: InterestUnknown, Currency: "USD", People: []string{}}
	values := sharedFieldMap(desired)
	if values["status"] == "" {
		values["status"] = StatusProspect
	}
	if values["interest"] == "" {
		values["interest"] = InterestUnknown
	}
	if values["currency"] == "" {
		values["currency"] = "USD"
	}
	validated, err := sharedWithFields(defaults, values)
	if err != nil {
		return Opportunity{}, err
	}
	website, err := normalizeWebsite(validated.Website)
	if err != nil {
		return Opportunity{}, err
	}
	validated.Website = website

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	op, err := s.create(validated.Firm)
	if err != nil {
		return Opportunity{}, err
	}
	op.Website = validated.Website
	op.Status = validated.Status
	op.Interest = validated.Interest
	op.Amount = validated.Amount
	op.Currency = validated.Currency
	op.LastTouchpoint = validated.LastTouchpoint
	op.LastTouchpointDate = validated.LastTouchpointDate
	op.NextStep = validated.NextStep
	op.NextStepDue = validated.NextStepDue
	op.Notes = validated.Notes
	op.People, op.UnlinkedPeople = s.resolveSharedPeople(validated.People, op)
	if validated.Source != "" {
		op.Source = &SourceRef{Text: validated.Source}
	}
	if err := s.replaceKnown(op); err != nil {
		return Opportunity{}, err
	}
	return op, nil
}

func (s *Store) resolveSharedPeople(names []string, current Opportunity) ([]PersonRef, []string) {
	currentByName := map[string]PersonRef{}
	for _, p := range current.People {
		currentByName[strings.ToLower(strings.TrimSpace(p.Display))] = p
	}
	registryByName := map[string][]RegistryPerson{}
	for _, p := range s.People() {
		key := strings.ToLower(strings.TrimSpace(p.Display))
		registryByName[key] = append(registryByName[key], p)
	}
	linked := []PersonRef{}
	plain := []string{}
	for _, name := range normalizePlainPeople(names) {
		key := strings.ToLower(name)
		if p, ok := currentByName[key]; ok {
			linked = append(linked, p)
			continue
		}
		if matches := registryByName[key]; len(matches) == 1 {
			p := matches[0]
			linked = append(linked, PersonRef{Key: p.Key, Display: p.Display, NotePath: p.NotePath})
			continue
		}
		plain = append(plain, name)
	}
	return mergePeople(nil, linked), normalizePlainPeople(plain)
}
