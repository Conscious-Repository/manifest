package gmailsend

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"testing"
	"time"
)

func TestAttachmentWireRoundTripAndDeterminism(t *testing.T) {
	data := []byte{0, 1, 2, 13, 10, 255}
	m := Message{From: "owner@example.test", To: []string{"recipient@example.test"}, Subject: "Plan review", Body: "Please review.\nNo execution.", Date: time.Unix(1000, 0), MessageID: "<fixed@example.test>", Attachments: []Attachment{{Name: "751 plans – revised.pdf", MediaType: "application/pdf", Data: data}}}
	raw, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Build(m)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatal("same approved input produced different wire bytes", err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	typ, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || typ != "multipart/mixed" {
		t.Fatal(typ, err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	for i, expected := range [][]byte{[]byte("Please review.\r\nNo execution."), data} {
		p, err := r.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, p))
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("part %d bytes changed: %q %v", i, got, err)
		}
		if i == 1 && p.FileName() != m.Attachments[0].Name {
			t.Fatal("filename changed", p.FileName())
		}
	}
	if _, err := r.NextPart(); err != io.EOF {
		t.Fatal("unexpected part", err)
	}
	if !bytes.Equal(data, m.Attachments[0].Data) {
		t.Fatal("input bytes mutated")
	}
}

func TestAttachmentValidation(t *testing.T) {
	for _, a := range []Attachment{{Name: "../secret"}, {Name: "bad\r\nheader.pdf"}, {Name: "ok.pdf", MediaType: "bad\r\ntype"}, {Name: "large.pdf", Data: make([]byte, maxAttachmentBytes+1)}} {
		if _, _, err := buildAttachments(Message{Attachments: []Attachment{a}}); err == nil {
			t.Fatal("invalid attachment accepted", a.Name)
		}
	}
}
