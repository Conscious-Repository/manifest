package portals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// verifyTypeSafe proves a TypeSafe key with one minimal Jev request: a single
// Noul question over a one-word state. 200 means the key works; 401 means it
// does not; anything else is reported as the service's own status.
func verifyTypeSafe(ctx context.Context, hc *http.Client, base, key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("no API key")
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	if base == "" {
		base = "https://api.typesafe.ai"
	}
	body, _ := json.Marshal(map[string]any{
		"state": "ok", "model": "jev-latest",
		"questions": map[string]any{"ping": map[string]any{"type": "noul", "instructions": "Is the text the word ok?"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	req.Header.Set("Content-Type", "application/json")
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	switch {
	case res.StatusCode == http.StatusOK:
		return nil
	case res.StatusCode == http.StatusUnauthorized:
		return errors.New("TypeSafe rejected the API key (401)")
	default:
		return fmt.Errorf("TypeSafe answered %s", res.Status)
	}
}
