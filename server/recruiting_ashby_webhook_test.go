package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/recruiting"
)

const testWebhookSecret = "whsec_ROUTE_SECRET_test_4242"

// sign is the sender's side of the contract: sha256=<hex of HMAC(secret, raw)>.
func sign(secret string, raw []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookPost(t *testing.T, s *Server, body, signature string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/aion/recruiting/ashby/webhook", strings.NewReader(body))
	if signature != "" {
		r.Header.Set(ashbySignatureHeader, signature)
	}
	w := httptest.NewRecorder()
	s.handleRecruitingAshbyWebhook(w, r)
	return w
}

// captureLog routes the standard logger into a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func decodeWebhook(t *testing.T, w *httptest.ResponseRecorder) recruiting.AshbyWebhookResult {
	t.Helper()
	var out struct {
		Webhook recruiting.AshbyWebhookResult `json:"webhook"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	return out.Webhook
}

const stageDelivery = `{"action":"candidateStageChange","id":"wh_evt_001","data":{"application":{"id":"app_1"},"candidate":{"id":"cand_r1"}}}`

// A correctly signed delivery runs the incremental sync-back (the three
// list calls) and answers 200 with the sync summary; the key lands in the
// derived state (dataDir, never the vault).
func TestRecruitingAshbyWebhookValidSignatureSyncs(t *testing.T) {
	s, fake, vault, _ := testAshbyServer(t, testPrivateKey)
	s.UseAshbyWebhookSecret(testWebhookSecret)
	logs := captureLog(t)

	w := webhookPost(t, s, stageDelivery, sign(testWebhookSecret, []byte(stageDelivery)))
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	res := decodeWebhook(t, w)
	if res.Duplicate || res.Sync == nil || res.Sync.Full || res.Action != "candidateStageChange" || res.Key != "candidateStageChange:wh_evt_001" {
		t.Fatalf("result: %+v", res)
	}
	for _, m := range []string{"jobPosting.list", "candidate.list", "application.list"} {
		if !fake.has(m) {
			t.Errorf("missing %s in %v", m, fake.calls)
		}
	}
	st := s.ashbySync.State()
	if len(st.Webhooks) != 1 || st.Webhooks[0] != res.Key || st.LastSync == "" {
		t.Fatalf("state: %+v", st)
	}
	if hits, _ := filepath.Glob(filepath.Join(vault, "**", "ashby.json")); len(hits) != 0 {
		t.Fatalf("sync state inside the vault: %v", hits)
	}
	for _, text := range []string{w.Body.String(), logs.String()} {
		if strings.Contains(text, testWebhookSecret) || strings.Contains(text, testPrivateKey) {
			t.Fatalf("secret leaked: %s", text)
		}
	}
}

// A bad or missing signature is a 401 before the body is even decoded:
// nothing reaches Ashby, nothing is recorded, and neither the response
// nor the log carries the secret.
func TestRecruitingAshbyWebhookRejectsBadSignature(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	s.UseAshbyWebhookSecret(testWebhookSecret)
	logs := captureLog(t)

	for name, sig := range map[string]string{
		"wrong secret": sign("someone-elses-secret", []byte(stageDelivery)),
		"tampered":     sign(testWebhookSecret, []byte(stageDelivery+" ")),
		"missing":      "",
		"garbage":      "sha256=not-hex",
	} {
		w := webhookPost(t, s, stageDelivery, sig)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("a rejected delivery reached Ashby: %v", fake.calls)
	}
	if st := s.ashbySync.State(); len(st.Webhooks) != 0 || st.LastSync != "" {
		t.Fatalf("a rejected delivery touched the state: %+v", st)
	}
	if out := logs.String(); !strings.Contains(out, "rejected delivery") || strings.Contains(out, testWebhookSecret) {
		t.Fatalf("log: %q", out)
	}
}

// No secret installed: verification is skipped (absent config is a state,
// not a failure) and the delivery is processed, signed or not.
func TestRecruitingAshbyWebhookNoSecretProcesses(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	w := webhookPost(t, s, stageDelivery, "")
	if w.Code != http.StatusOK {
		t.Fatalf("unsigned without secret: %d %s", w.Code, w.Body.String())
	}
	if res := decodeWebhook(t, w); res.Duplicate || res.Sync == nil {
		t.Fatalf("result: %+v", res)
	}
	if !fake.has("candidate.list") {
		t.Fatalf("not processed: %v", fake.calls)
	}
}

// A redelivery of the same key is acknowledged (200, duplicate:true) and
// runs nothing: the call count does not move and the key is recorded once.
// A payload without an id dedupes on the body hash the same way.
func TestRecruitingAshbyWebhookRedeliveryIsIdempotent(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	s.UseAshbyWebhookSecret(testWebhookSecret)
	sig := sign(testWebhookSecret, []byte(stageDelivery))

	if w := webhookPost(t, s, stageDelivery, sig); w.Code != http.StatusOK {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}
	n := len(fake.calls)
	w := webhookPost(t, s, stageDelivery, sig)
	if w.Code != http.StatusOK {
		t.Fatalf("redelivery: %d %s", w.Code, w.Body.String())
	}
	if res := decodeWebhook(t, w); !res.Duplicate || res.Sync != nil {
		t.Fatalf("redelivery result: %+v", res)
	}
	if len(fake.calls) != n {
		t.Fatalf("redelivery re-ran the sync: %v", fake.calls[n:])
	}
	if st := s.ashbySync.State(); len(st.Webhooks) != 1 {
		t.Fatalf("keys: %v", st.Webhooks)
	}

	anon := `{"action":"applicationSubmit","data":{"application":{"id":"app_9"}}}`
	for i, want := range []bool{false, true} {
		w := webhookPost(t, s, anon, sign(testWebhookSecret, []byte(anon)))
		if w.Code != http.StatusOK {
			t.Fatalf("anon %d: %d %s", i, w.Code, w.Body.String())
		}
		res := decodeWebhook(t, w)
		if res.Duplicate != want || !strings.HasPrefix(res.Key, "applicationSubmit:sha256:") {
			t.Fatalf("anon %d: %+v", i, res)
		}
	}
	if st := s.ashbySync.State(); len(st.Webhooks) != 2 {
		t.Fatalf("keys: %v", st.Webhooks)
	}
}

// The sync failing is the dead-letter path: a 5xx so Ashby retries, and
// nothing recorded, so the retry actually re-runs. The upstream error text
// is redacted by the client; the webhook secret is never in it.
func TestRecruitingAshbyWebhookSyncFailureIs5xx(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	s.UseAshbyWebhookSecret(testWebhookSecret)
	logs := captureLog(t)
	fake.failWith = "api key revoked " + testPrivateKey
	sig := sign(testWebhookSecret, []byte(stageDelivery))

	w := webhookPost(t, s, stageDelivery, sig)
	if w.Code < 500 || w.Code > 599 {
		t.Fatalf("failed sync: %d %s", w.Code, w.Body.String())
	}
	if st := s.ashbySync.State(); len(st.Webhooks) != 0 {
		t.Fatalf("a failed delivery was recorded: %v", st.Webhooks)
	}
	for _, text := range []string{w.Body.String(), logs.String()} {
		if strings.Contains(text, testWebhookSecret) || strings.Contains(text, testPrivateKey) {
			t.Fatalf("secret leaked: %s", text)
		}
	}
	if !strings.Contains(logs.String(), "delivery failed") {
		t.Fatalf("no failure log: %q", logs.String())
	}

	// the retry, once upstream recovers, runs and records
	fake.failWith = ""
	w = webhookPost(t, s, stageDelivery, sig)
	if w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	if res := decodeWebhook(t, w); res.Duplicate || res.Sync == nil {
		t.Fatalf("retry result: %+v", res)
	}
}

// Without an API key there is nothing to reconcile against: the delivery
// is acknowledged (a retry could not help) and nothing is recorded.
func TestRecruitingAshbyWebhookUnconfiguredClientAcks(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, "")
	captureLog(t)
	w := webhookPost(t, s, stageDelivery, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ignored":"unconfigured"`) {
		t.Fatalf("unconfigured: %d %s", w.Code, w.Body.String())
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unconfigured client called Ashby: %v", fake.calls)
	}
	if st := s.ashbySync.State(); len(st.Webhooks) != 0 {
		t.Fatalf("recorded: %v", st.Webhooks)
	}
}

// A ping and a non-JSON body: the connectivity check is a 200 that runs
// nothing; garbage is a 400 that runs nothing.
func TestRecruitingAshbyWebhookPingAndBadBody(t *testing.T) {
	s, fake, _, _ := testAshbyServer(t, testPrivateKey)
	s.UseAshbyWebhookSecret(testWebhookSecret)
	ping := `{"action":"ping"}`
	w := webhookPost(t, s, ping, sign(testWebhookSecret, []byte(ping)))
	if w.Code != http.StatusOK {
		t.Fatalf("ping: %d %s", w.Code, w.Body.String())
	}
	if res := decodeWebhook(t, w); res.Action != "ping" || res.Sync != nil {
		t.Fatalf("ping result: %+v", res)
	}
	bad := `not json`
	if w := webhookPost(t, s, bad, sign(testWebhookSecret, []byte(bad))); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d %s", w.Code, w.Body.String())
	}
	if len(fake.calls) != 0 {
		t.Fatalf("ping/bad body reached Ashby: %v", fake.calls)
	}
}

// The recorded key set is bounded like the audit tail.
func TestRecruitingAshbyWebhookKeySetIsBounded(t *testing.T) {
	s, _, _, _ := testAshbyServer(t, testPrivateKey)
	path := s.ashbySync.StatePath()
	st := s.ashbySync.State()
	for i := 0; i < 1000; i++ {
		st.Webhooks = append(st.Webhooks, "seed:"+hex.EncodeToString([]byte{byte(i >> 8), byte(i)}))
	}
	b, _ := json.Marshal(st)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if w := webhookPost(t, s, stageDelivery, ""); w.Code != http.StatusOK {
		t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	got := s.ashbySync.State().Webhooks
	if len(got) != 1000 || got[len(got)-1] != "candidateStageChange:wh_evt_001" || got[0] == "seed:0000" {
		t.Fatalf("bound: len=%d first=%s last=%s", len(got), got[0], got[len(got)-1])
	}
}
