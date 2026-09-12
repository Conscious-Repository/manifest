package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"manifest/agentchat"
	"manifest/chatthreads"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type terminalInput struct {
	QuestionAnswers   []terminalQuestionAnswer `json:"questionAnswers,omitempty"`
	Files             []string                 `json:"files,omitempty"`
	Text              string                   `json:"text"`
	Key               string                   `json:"key"`
	Supervise         bool                     `json:"supervise"`
	TimeoutMS         int                      `json:"timeoutMs"`
	Task              string                   `json:"task"`
	Artifacts         []artifactContextRef     `json:"artifacts"`
	RequestID         string                   `json:"requestId"`
	ConversationAgent string                   `json:"conversationAgent,omitempty"`
	ConversationID    string                   `json:"conversationId,omitempty"`
}

func (b terminalInput) fingerprint() string {
	b.RequestID = ""
	raw, _ := json.Marshal(b)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// This is a submission receipt, not a copied transcript or a claim that the
// agent completed work. Persist unconfirmed BEFORE crossing the runtime boundary.
// A lost reply/crash leaves uncertainty that must never authorize replay.
type terminalInputReceipt struct {
	Error           string                   `json:"error,omitempty"`
	QuestionAnswers []terminalQuestionAnswer `json:"questionAnswers,omitempty"`
	Files           []chatthreads.FileRef    `json:"files,omitempty"`
	SharedAgent     string                   `json:"sharedAgent,omitempty"`
	SharedThread    string                   `json:"sharedThread,omitempty"`
	ActorEmail      string                   `json:"actorEmail,omitempty"`
	ActorName       string                   `json:"actorName,omitempty"`
	ID              string                   `json:"id"`
	Fingerprint     string                   `json:"fingerprint"`
	State           string                   `json:"state"` // unconfirmed | sent
	Updated         string                   `json:"updated"`
	Runtime         terminalIdentity         `json:"runtime"`
	Task            string                   `json:"task,omitempty"`
	Artifacts       []artifactContextRef     `json:"artifacts,omitempty"`
	Text            string                   `json:"text,omitempty"`
	SubmittedHash   string                   `json:"submittedHash,omitempty"`
	ContextSource   string                   `json:"contextSource,omitempty"`
	ContextHash     string                   `json:"contextHash,omitempty"`
	HistoryOmitted  int                      `json:"historyOmitted,omitempty"`
}

func hashTerminalText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Codex may remove the final line terminator before recording an input. Match
// only the exact recorded text or that text with one final LF restored; never
// trim, fuzzy-match, or infer ownership from a submission ID in model text.
func matchingInputReceipt(receipts map[string]terminalInputReceipt, text string) (terminalInputReceipt, bool) {
	if r, ok := receipts[hashTerminalText(text)]; ok {
		return r, true
	}
	r, ok := receipts[hashTerminalText(text+"\n")]
	return r, ok
}

func (c *termCfg) continuationReceipts(id, source string) map[string]terminalInputReceipt {
	out := map[string]terminalInputReceipt{}
	entries, err := os.ReadDir(filepath.Join(c.regPath+".inputs", id))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		request := e.Name()[:len(e.Name())-5]
		if !agentchat.ValidRequestID(request) {
			continue
		}
		r, err := c.readInputReceipt(id, request)
		if err == nil && (source == "" || r.ContextSource == source) && len(r.SubmittedHash) == 64 {
			out[r.SubmittedHash] = r
		}
	}
	return out
}

func (c *termCfg) inputReceiptPath(id, request string) string {
	return filepath.Join(c.regPath+".inputs", id, request+".json")
}
func (c *termCfg) readInputReceipt(id, request string) (terminalInputReceipt, error) {
	var out terminalInputReceipt
	raw, err := os.ReadFile(c.inputReceiptPath(id, request))
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.ID != request || (out.State != "sent" && out.State != "unconfirmed") || len(out.Fingerprint) != 64 {
		return out, errors.New("invalid input receipt; no input sent")
	}
	return out, nil
}

// Caller holds the terminal's stable ID input mutex. Atomic persistence ensures
// a concurrent status read sees either complete version, never a partial receipt.
func (c *termCfg) writeInputReceipt(id string, receipt terminalInputReceipt) error {
	receipt.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	path := c.inputReceiptPath(id, receipt.ID)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".input-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func writeTerminalInputReceipt(w http.ResponseWriter, id string, receipt terminalInputReceipt) {
	if receipt.State != "sent" {
		w.WriteHeader(http.StatusAccepted)
	}
	writeJSON(w, map[string]any{"ok": receipt.State == "sent", "id": id, "delivery": receipt})
}

func (s *Server) handleTermDelivery(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	request := r.URL.Query().Get("request")
	if !agentchat.ValidRequestID(request) {
		httpError(w, errBadRequest("invalid request ID"))
		return
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, request)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeTerminalInputReceipt(w, se.ID, receipt)
}
