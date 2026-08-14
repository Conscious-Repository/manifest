package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// STT (cmd-ctr import P6): the mic buttons' dictation proxy. The browser
// records an utterance (WAV, encoded client-side) and posts it here; manifest
// relays to the LAB's self-hosted transcription service (granite-speech on
// the 8000–8099 range — reachable from metis only, no cloud keys). The
// endpoint is config (labSttUrl); unset = a clear "dictation not configured"
// error the mic button surfaces.

// UseSTT wires the lab endpoint + model.
func (s *Server) UseSTT(url, model string) { s.sttURL, s.sttModel = url, model }

func (s *Server) handleSTT(w http.ResponseWriter, r *http.Request) {
	if s.sttURL == "" {
		http.Error(w, "dictation not configured (set labSttUrl)", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	audio, err := io.ReadAll(r.Body)
	if err != nil || len(audio) == 0 {
		httpError(w, errBadRequest("no audio"))
		return
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "utterance.wav")
	if err != nil {
		httpError(w, err)
		return
	}
	if _, err := fw.Write(audio); err != nil {
		httpError(w, err)
		return
	}
	_ = mw.WriteField("model", s.sttModel)
	_ = mw.Close()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.sttURL, &buf)
	if err != nil {
		httpError(w, err)
		return
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "lab STT unreachable — "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("lab STT %d: %.300s", resp.StatusCode, body), http.StatusBadGateway)
		return
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Text == "" {
		// tolerate wrapped shapes; surface raw on miss
		http.Error(w, "lab STT returned no text", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"text": out.Text})
}
