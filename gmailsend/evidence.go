package gmailsend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"reflect"
	"strings"
)

var ErrEnvelopeEvidence = errors.New("sent evidence does not match the approved envelope")

// MatchSentEnvelope checks the complete authored message, including attachments.
// Only transport headers and MIME transport representations may differ. Unknown
// authored headers, duplicate headers, changed recipients or content fail closed.
func MatchSentEnvelope(m Message, raw []byte) error {
	if m.Date.IsZero() || m.MessageID == "" || len(raw) > 32<<20 {
		return ErrEnvelopeEvidence
	}
	expected, err := Build(m)
	if err != nil {
		return ErrEnvelopeEvidence
	}
	a, err := mail.ReadMessage(bytes.NewReader(expected))
	if err != nil {
		return ErrEnvelopeEvidence
	}
	b, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ErrEnvelopeEvidence
	}
	ah, err := evidenceHeaders(textproto.MIMEHeader(a.Header), true)
	if err != nil {
		return ErrEnvelopeEvidence
	}
	bh, err := evidenceHeaders(textproto.MIMEHeader(b.Header), true)
	if err != nil || !reflect.DeepEqual(ah, bh) {
		return ErrEnvelopeEvidence
	}
	if !matchMIME(textproto.MIMEHeader(a.Header), a.Body, textproto.MIMEHeader(b.Header), b.Body, 0) {
		return ErrEnvelopeEvidence
	}
	return nil
}

func evidenceHeaders(h textproto.MIMEHeader, top bool) (map[string]string, error) {
	out := map[string]string{}
	for key, vs := range h {
		k := strings.ToLower(key)
		if top && (k == "received" || k == "return-path" || k == "delivered-to" || k == "authentication-results" || k == "dkim-signature" || strings.HasPrefix(k, "arc-") || strings.HasPrefix(k, "x-google-") || strings.HasPrefix(k, "x-gm-") || k == "x-received") {
			continue
		}
		if len(vs) != 1 {
			return nil, ErrEnvelopeEvidence
		}
		v := strings.TrimSpace(vs[0])
		switch k {
		case "content-transfer-encoding":
			continue // compared after decoding
		case "content-type", "content-disposition":
			typ, params, err := mime.ParseMediaType(v)
			if err != nil {
				return nil, err
			}
			if k == "content-type" {
				if strings.HasPrefix(typ, "multipart/") {
					delete(params, "boundary")
				}
				if charset, ok := params["charset"]; ok {
					params["charset"] = strings.ToLower(charset)
				}
			}
			v = mime.FormatMediaType(typ, params)
		case "from", "to", "cc":
			addrs, err := mail.ParseAddressList(v)
			if err != nil {
				return nil, err
			}
			encoded, _ := json.Marshal(addrs)
			v = string(encoded)
		case "subject":
			decoded, err := new(mime.WordDecoder).DecodeHeader(v)
			if err != nil {
				return nil, err
			}
			v = decoded
		case "date":
			date, err := mail.ParseDate(v)
			if err != nil {
				return nil, err
			}
			v = date.UTC().String()
		}
		out[k] = v
	}
	return out, nil
}

func decodeEvidence(h textproto.MIMEHeader, r io.Reader) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "", "7bit", "8bit", "binary":
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	default:
		return nil, ErrEnvelopeEvidence
	}
	b, err := io.ReadAll(io.LimitReader(r, (32<<20)+1))
	if err != nil || len(b) > 32<<20 {
		return nil, ErrEnvelopeEvidence
	}
	return b, nil
}

func matchMIME(ah textproto.MIMEHeader, ar io.Reader, bh textproto.MIMEHeader, br io.Reader, depth int) bool {
	if depth > 1 {
		return false
	} // Builder emits only one mixed container.
	at, ap, err := mime.ParseMediaType(ah.Get("Content-Type"))
	if err != nil {
		return false
	}
	bt, bp, err := mime.ParseMediaType(bh.Get("Content-Type"))
	if err != nil || at != bt {
		return false
	}
	if strings.HasPrefix(at, "multipart/") {
		if at != "multipart/mixed" || ap["boundary"] == "" || bp["boundary"] == "" || ah.Get("Content-Transfer-Encoding") != "" || bh.Get("Content-Transfer-Encoding") != "" {
			return false
		}
		am, bm := multipart.NewReader(ar, ap["boundary"]), multipart.NewReader(br, bp["boundary"])
		for i := 0; i < 22; i++ {
			a, ae := am.NextRawPart()
			b, be := bm.NextRawPart()
			if ae == io.EOF && be == io.EOF {
				return true
			}
			if ae != nil || be != nil {
				return false
			}
			x, xe := evidenceHeaders(a.Header, false)
			y, ye := evidenceHeaders(b.Header, false)
			if xe != nil || ye != nil || !reflect.DeepEqual(x, y) || !matchMIME(a.Header, a, b.Header, b, depth+1) {
				return false
			}
		}
		return false
	}
	a, ae := decodeEvidence(ah, ar)
	b, be := decodeEvidence(bh, br)
	return ae == nil && be == nil && bytes.Equal(a, b)
}
