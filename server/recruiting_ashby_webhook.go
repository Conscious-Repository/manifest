package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"manifest/recruiting"
)

// The Ashby WEBHOOK RECEIVER (Phase 7). Ashby changes reach Manifest without
// a poller: a delivery is a signal, and the only thing it triggers is the
// existing incremental sync-back (recruiting.AshbySync.HandleWebhook →
// syncBack). Nothing here reads a record out of the payload — Ashby is
// re-fetched through the authenticated client, under the same
// drift/conflict rules the owner's sync route uses.
//
//	POST /api/aion/recruiting/ashby/webhook   the receiver (mounted beside the Phase 6 routes)
//
// Verification: Ashby signs the RAW JSON body with `Ashby-Signature:
// sha256=<hex>` = HMAC-SHA256(secret, body) when a secret is set under
// Admin → Integrations → Webhooks. The secret's ONE source is the
// ASHBY_WEBHOOK_SECRET environment variable (main.go, beside ASHBY_API_KEY);
// with it installed, a missing or mismatching signature is a 401 and the
// body is never decoded. Without it the receiver FAILS CLOSED: every
// delivery is a 503 "webhook receiver not configured", the body is not
// read, and nothing runs — an unauthenticated POST that could trigger
// Ashby reads and record writes is not a posture, it is a hole. The
// secret is never logged, echoed, or compared with anything but
// hmac.Equal.
//
// Idempotency: each delivery has a key (Ashby's own id when the payload
// carries one, else the body's SHA-256), recorded in the derived sync
// state only after a successful sync. A redelivery answers 200 with
// duplicate:true and runs nothing. A failed sync is a 5xx and records
// nothing, so the sender's retry re-runs it.
//
// The sync runs IN the request (bounded by ashbyCtx's 45s), and the 200
// carries its summary. There is no queue and no goroutine: Ashby retries
// on a non-2xx, which is the whole dead-letter story.

// maxAshbyWebhookBody bounds the raw read: a delivery is a small JSON
// envelope, and the body is only hashed and skimmed for a key.
const maxAshbyWebhookBody = 1 << 20

const ashbySignatureHeader = "Ashby-Signature"

// UseAshbyWebhookSecret installs the delivery-signing secret (from
// ASHBY_WEBHOOK_SECRET). Empty leaves the receiver closed (503).
func (s *Server) UseAshbyWebhookSecret(secret string) { s.ashbyWebhookSecret = secret }

// ashbySignatureOK checks `sha256=<hex>` (the prefix is optional) against
// HMAC-SHA256(secret, raw). Only hmac.Equal touches the bytes.
func ashbySignatureOK(secret string, raw []byte, header string) bool {
	sig := strings.TrimSpace(header)
	sig = strings.TrimPrefix(sig, "sha256=")
	got, err := hex.DecodeString(sig)
	if err != nil || len(got) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	return hmac.Equal(got, mac.Sum(nil))
}

// ashbyWebhookEnvelope is the little we read off a delivery: its action and
// whichever id field Ashby put on it. Everything else is opaque.
type ashbyWebhookEnvelope struct {
	Action     string          `json:"action"`
	ID         string          `json:"id"`
	WebhookID  string          `json:"webhookId"`
	DeliveryID string          `json:"deliveryId"`
	EventID    string          `json:"eventId"`
	Data       json.RawMessage `json:"data"`
}

// ashbyWebhookKey is the dedupe key: Ashby's delivery id when present, else
// the SHA-256 of the raw body (a redelivery is byte-identical). The action
// is folded in front so the same id under a different action is distinct.
func ashbyWebhookKey(env ashbyWebhookEnvelope, raw []byte) string {
	for _, id := range []string{env.ID, env.DeliveryID, env.EventID, env.WebhookID} {
		if id = strings.TrimSpace(id); id != "" {
			return strings.TrimSpace(env.Action) + ":" + id
		}
	}
	sum := sha256.Sum256(raw)
	return strings.TrimSpace(env.Action) + ":sha256:" + hex.EncodeToString(sum[:])
}

// POST …/ashby/webhook → verify, dedupe, sync-back. Answers:
//
//	503  no signing secret installed (fail closed; body not read)
//	401  signature missing/mismatched (secret installed; nothing processed)
//	400  not JSON
//	200  {"webhook":{key,action,duplicate,sync?}} — also for a ping and for
//	     an unconfigured client (nothing to sync against; nothing recorded)
//	5xx  the sync failed; nothing recorded, so a retry re-runs it
func (s *Server) handleRecruitingAshbyWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	if s.ashbyWebhookSecret == "" {
		http.Error(w, "webhook receiver not configured", http.StatusServiceUnavailable)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAshbyWebhookBody+1))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(raw) > maxAshbyWebhookBody {
		http.Error(w, "webhook body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !ashbySignatureOK(s.ashbyWebhookSecret, raw, r.Header.Get(ashbySignatureHeader)) {
		log.Printf("recruiting ashby webhook: rejected delivery (bad or missing %s)", ashbySignatureHeader)
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}
	var env ashbyWebhookEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		http.Error(w, "a webhook delivery is a JSON object", http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(env.Action)
	key := ashbyWebhookKey(env, raw)
	if action == "ping" {
		// Ashby's connectivity check on webhook creation: nothing changed.
		writeJSON(w, map[string]any{"webhook": recruiting.AshbyWebhookResult{Key: key, Action: action}})
		return
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	res, err := s.ashbySync.HandleWebhook(ctx, key, action, time.Now())
	switch {
	case err == nil:
	case errors.Is(err, recruiting.ErrAshbyUnconfigured):
		// No API key: there is nothing to reconcile against, and a retry
		// would not change that. Ack so Ashby stops resending; the owner's
		// next sync catches up through the stored syncTokens.
		log.Printf("recruiting ashby webhook: %s delivery ignored — %v", action, err)
		writeJSON(w, map[string]any{"webhook": res, "ignored": "unconfigured"})
		return
	default:
		// A 5xx is the dead-letter path: Ashby retries, and nothing was
		// recorded, so the retry re-runs the sync. The client redacted the
		// key before the error existed; the secret never enters an error.
		log.Printf("recruiting ashby webhook: %s delivery failed — %v", action, err)
		var api *recruiting.AshbyError
		status := http.StatusInternalServerError
		if errors.As(err, &api) {
			status = http.StatusBadGateway
		}
		http.Error(w, fmt.Sprintf("webhook sync failed: %v", err), status)
		return
	}
	if res.Duplicate {
		log.Printf("recruiting ashby webhook: %s redelivery %s skipped", action, key)
	}
	writeJSON(w, map[string]any{"webhook": res})
}
