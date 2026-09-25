package contacts

import "sort"

// Organizations projects only the owner's explicit org/firm classifications.
// It does not infer organizations from links, names or contact roles.
func (s *Service) Organizations() []Ref {
	s.store.mu.Lock()
	keys := []string{}
	for key, marked := range s.store.st.Orgs {
		if marked {
			keys = append(keys, key)
		}
	}
	s.store.mu.Unlock()
	sort.Strings(keys)
	out := []Ref{}
	for _, key := range keys {
		row := Ref{Key: key, Display: key}
		if e, ok := s.ix.Entity(key); ok {
			row.Display = e.Display
			row.NotePath = e.NotePath
			row.HasNote = e.HasNote
		}
		out = append(out, row)
	}
	return out
}
