package server

import (
	"bytes"
	"manifest/artifacts"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArtifactPreviewSelectedBytes(t *testing.T) {
	s, _, _ := artifactFixture(t)
	binary := []byte{'P', 'K', 3, 4, 0, 0xff}
	a, err := s.artifactReg.Put(artifacts.Put{Ref: "misleading.txt", Content: binary})
	if err != nil {
		t.Fatal(err)
	}
	old := a.Artifact.Head
	a, err = s.artifactReg.Put(artifacts.Put{ID: a.Artifact.ID, Content: []byte("current text"), ExpectedHead: old})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		rev, kind, content string
		size               int
	}{{old, "metadata", "", len(binary)}, {a.Artifact.Head, "text", "current text", 12}} {
		code, v := artifactsDo(t, s, "GET", "/api/artifacts/get?id="+a.Artifact.ID+"&preview=1&rev="+tt.rev, "")
		if code != 200 {
			t.Fatalf("%d %+v", code, v)
		}
		p := v["preview"].(map[string]any)
		if p["revision"] != tt.rev || p["kind"] != tt.kind || p["size"] != float64(tt.size) {
			t.Fatal(p)
		}
		if tt.content == "" {
			if _, exists := v["content"]; exists {
				t.Fatal("binary decoded as JSON text")
			}
		} else if v["content"] != tt.content {
			t.Fatal(v)
		}
	}
	if code, _ := artifactsDo(t, s, "GET", "/api/artifacts/get?id="+a.Artifact.ID+"&preview=1&rev="+strings.Repeat("0", 64), ""); code != 404 {
		t.Fatal(code)
	}
	rr := httptest.NewRecorder()
	s.handleArtifactContent(rr, httptest.NewRequest("GET", "/api/artifacts/content?id="+a.Artifact.ID+"&rev="+old, nil))
	if rr.Code != 200 || !bytes.Equal(rr.Body.Bytes(), binary) || rr.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(rr.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal(rr.Code, rr.Header())
	}
}

func TestArtifactPreviewTypesAndLimits(t *testing.T) {
	for _, tt := range []struct{ content, kind string }{
		{"<script>alert(1)</script>", "text"}, {"é", "text"}, {string([]byte{0xff}), "metadata"}, {"binary\x01", "metadata"},
		{strings.Repeat("é", 524289), "metadata"}, {strings.Repeat("x", 1048576), "text"},
		{"%PDF-1.7\n", "pdf"}, {"\x89PNG\r\n\x1a\n", "image"}, {"GIF89a", "image"},
	} {
		p := describeArtifactPreview("version", []byte(tt.content))
		if p.Kind != tt.kind || p.Size != len(tt.content) || p.Revision != "version" {
			t.Fatalf("%q: %+v", tt.content[:min(len(tt.content), 16)], p)
		}
		if tt.kind == "metadata" && p.Reason == "" {
			t.Fatal("missing limitation")
		}
	}
}
