package writing

import (
	"encoding/json"
	"errors"
	"manifest/record"
	"manifest/vaultwriter"
	"path"
	"strings"
)

type Sources struct {
	Folders  []string `json:"folders"`
	Revision string   `json:"revision"`
}

func parseSources(raw []byte) (Sources, error) {
	s := Sources{Folders: []string{}, Revision: vaultwriter.Revision(raw)}
	blocks, err := record.JSONBlocks(string(raw), "manifest-writing-sources")
	if err != nil {
		return s, err
	}
	if len(raw) > 0 && len(blocks) != 1 {
		return s, errors.New("invalid source collection record")
	}
	if len(blocks) == 1 {
		err = json.Unmarshal(blocks[0], &s.Folders)
	}
	return s, err
}
func (s *Store) Sources() (Sources, error) {
	b, err := s.Writer.ReadVaultFile(path.Join(s.Root, "sources.md"))
	if err != nil && !isMissing(err) {
		return Sources{}, err
	}
	return parseSources(b)
}
func (s *Store) SetSources(folders []string, expected string) (Sources, error) {
	var result Sources
	err := s.Writer.UpdateCap("writing", path.Join(s.Root, "sources.md"), func(raw []byte) ([]byte, error) {
		current, err := parseSources(raw)
		if err != nil {
			return nil, err
		}
		if current.Revision != expected {
			return nil, errors.New("source collection changed; reload")
		}
		text := string(raw)
		if len(raw) == 0 {
			text = "# Writing source collection\n"
			text, err = record.AppendJSONBlock(text, "manifest-writing-sources", folders)
		} else {
			blocks, _ := record.JSONBlocks(text, "manifest-writing-sources")
			b, _ := json.Marshal(folders)
			text = strings.Replace(text, string(blocks[0]), string(b), 1)
		}
		if err != nil {
			return nil, err
		}
		result, err = parseSources([]byte(text))
		return []byte(text), err
	})
	return result, err
}
