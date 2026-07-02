package vault

import (
	"bufio"
	"os"
	"strings"
)

// frontmatterType cheaply extracts the `type:` scalar from a leading YAML
// frontmatter block (a file whose first line is exactly "---"). It reads only up
// to the closing fence and never loads the whole file, so it is cheap to call on
// every non-daily note. Returns "" when there is no frontmatter or no type key.
//
// This deliberately understands only flat `key: value` scalars — the one field
// the scanner needs — instead of pulling in a YAML dependency.
func frontmatterType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() {
		return ""
	}
	if strings.TrimRight(sc.Text(), "\r") != "---" {
		return "" // no frontmatter fence
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "---" || line == "..." {
			return "" // closing fence, no type found
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:i]), "type") {
			return strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
		}
	}
	return ""
}
