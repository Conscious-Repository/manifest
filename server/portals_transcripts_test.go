package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"manifest/transcriptsync"
)

func TestTranscriptPortalUsesManifestCredentials(t *testing.T) {
	t.Setenv("POCKET_API_KEY", "")
	svc := transcriptsync.New(t.TempDir(), transcriptsync.Config{Pocket: transcriptsync.SourceConfig{Enabled: true, Account: "fixture"}}, nil, nil)
	srv := &Server{}
	srv.UseTranscriptSync(svc)
	req := httptest.NewRequest("POST", "/api/portals/heypocket/key", strings.NewReader(`{"fields":{"apiKey":"pk_fixture-private"}}`))
	req.SetPathValue("id", "heypocket")
	rec := httptest.NewRecorder()
	srv.handlePortalKey(rec, req)
	if rec.Code != 200 || !svc.HasKey("pocket") || strings.Contains(rec.Body.String(), "pk_fixture-private") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	row := srv.transcriptPortalRow("pocket")
	if row.Engine || row.ID != "heypocket" || !strings.Contains(row.Note, "Manifest") {
		t.Fatal(row)
	}
	req = httptest.NewRequest("POST", "/api/portals/heypocket/disconnect", nil)
	req.SetPathValue("id", "heypocket")
	rec = httptest.NewRecorder()
	srv.handlePortalDisconnect(rec, req)
	if rec.Code != 200 || svc.HasKey("pocket") {
		t.Fatal("disconnect failed", rec.Code)
	}
}
