package gmailsend

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"unicode"
)

// Attachment contains already-selected bytes. The mail builder never resolves
// a path or fetches a URL; callers must authorize and snapshot files first.
type Attachment struct {
	Name      string
	MediaType string
	Data      []byte
}

const maxAttachmentBytes = 20 << 20

func buildAttachments(m Message) (string, []byte, error) {
	if len(m.Attachments) > 20 {
		return "", nil, fmt.Errorf("at most 20 attachments are supported")
	}
	total := 0
	for _, a := range m.Attachments {
		if strings.TrimSpace(a.Name) == "" || strings.ContainsAny(a.Name, "/\\") || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 {
			return "", nil, fmt.Errorf("attachment needs a filename without path or control characters")
		}
		total += len(a.Data)
		if total > maxAttachmentBytes {
			return "", nil, fmt.Errorf("attachments exceed 20 MiB")
		}
		if a.MediaType != "" {
			if typ, _, err := mime.ParseMediaType(a.MediaType); err != nil || !strings.Contains(typ, "/") || strings.ContainsAny(a.MediaType, "\r\n") {
				return "", nil, fmt.Errorf("invalid attachment media type")
			}
		}
	}
	// The same approved content produces the same MIME boundary and bytes.
	seed, _ := json.Marshal(struct {
		Body  string
		Files []Attachment
	}{m.Body, m.Attachments})
	hash := sha256.Sum256(seed)
	boundary := fmt.Sprintf("manifest-%x", hash[:24])
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if err := w.SetBoundary(boundary); err != nil {
		return "", nil, err
	}
	part := func(headers textproto.MIMEHeader, data []byte) error {
		p, err := w.CreatePart(headers)
		if err != nil {
			return err
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		for len(encoded) > 0 {
			n := min(76, len(encoded))
			if _, err := fmt.Fprint(p, encoded[:n]+"\r\n"); err != nil {
				return err
			}
			encoded = encoded[n:]
		}
		return nil
	}
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
	if err := part(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}}, []byte(body)); err != nil {
		return "", nil, err
	}
	for _, a := range m.Attachments {
		mediaType := a.MediaType
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		headers := textproto.MIMEHeader{"Content-Type": {mediaType}, "Content-Transfer-Encoding": {"base64"}, "Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": a.Name})}}
		if err := part(headers, a.Data); err != nil {
			return "", nil, err
		}
	}
	if err := w.Close(); err != nil {
		return "", nil, err
	}
	return mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": boundary}), b.Bytes(), nil
}
