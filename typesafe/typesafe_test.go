package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "ts-test-key-not-real"

func stub(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body map[string]any)) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		h(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func tierReq() Request {
	return Request{
		State: map[string]any{"transcript": "hello"},
		Questions: map[string]Question{
			"tier": Choice("Which tier?", map[string]any{"open": "a", "internal": "b", "held": "c"}),
			"comp": Noul("Pay?", "yes pay", ""),
			"risk": Score("Risk?", "low", "high"),
		},
	}
}

func TestTypeSafeClientRequestShapeAndAnswers(t *testing.T) {
	srv, _ := stub(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		if body["model"] != DefaultModel {
			t.Errorf("model = %v, want %s", body["model"], DefaultModel)
		}
		qs := body["questions"].(map[string]any)
		tier := qs["tier"].(map[string]any)
		if tier["type"] != "choice" || tier["criteria"].(map[string]any)["held"] != "c" {
			t.Errorf("tier question = %v", tier)
		}
		if qs["comp"].(map[string]any)["criteria"].(map[string]any)["true"] != "yes pay" {
			t.Errorf("noul criteria = %v", qs["comp"])
		}
		if lv := qs["risk"].(map[string]any)["criteria"].([]any); len(lv) != 2 {
			t.Errorf("score levels = %v", lv)
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.13.0","answers":{
			"tier":{"type":"choice","choice":"held","probabilities":{"open":0.1,"internal":0.2,"held":0.7},"confidence":0.6},
			"comp":{"type":"noul","noul":0.0},
			"risk":{"type":"score","score":0.4,"probabilities":{"0":0.6,"1":0.4},"confidence":0.3,"legend":{"0":"low","1":"high"}}},
			"usage":{"input_tokens":12,"output_tokens":3}}`)
	})
	c := &Client{Key: func() string { return testKey }, BaseURL: srv.URL}
	res, err := c.Evaluate(context.Background(), tierReq())
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "jev-1.13.0" || res.Usage.InputTokens != 12 {
		t.Errorf("meta = %+v", res)
	}
	if a := res.Answers["tier"]; a.Choice != "held" || a.Probabilities["held"] != 0.7 || a.Confidence != 0.6 {
		t.Errorf("tier = %+v", a)
	}
	if a := res.Answers["comp"]; a.Noul == nil || *a.Noul != 0 {
		t.Errorf("a real 0 noul must survive: %+v", a)
	}
	if a := res.Answers["risk"]; a.Score == nil || *a.Score != 0.4 || a.Legend["1"] != "high" {
		t.Errorf("risk = %+v", a)
	}
}

func TestTypeSafeMissingKeyIsDisabledWithoutNetwork(t *testing.T) {
	srv, hits := stub(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {})
	for _, c := range []*Client{
		{BaseURL: srv.URL},
		{Key: func() string { return "   " }, BaseURL: srv.URL},
	} {
		if _, err := c.Evaluate(context.Background(), tierReq()); !errors.Is(err, ErrDisabled) {
			t.Errorf("err = %v, want ErrDisabled", err)
		}
		if c.Enabled() {
			t.Error("Enabled() with no key")
		}
	}
	if *hits != 0 {
		t.Errorf("a keyless client reached the network %d times", *hits)
	}
	if ErrDisabled.Error() != "TYPESAFE_API_KEY is not set" {
		t.Errorf("ErrDisabled = %q", ErrDisabled)
	}
}

func TestTypeSafeErrorsNeverCarryTheKey(t *testing.T) {
	srv, _ := stub(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"invalid api key"}`)
	})
	c := &Client{Key: func() string { return testKey }, BaseURL: srv.URL}
	_, err := c.Evaluate(context.Background(), tierReq())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks the key: %v", err)
	}
}

func TestTypeSafeRetriesRateLimitThenSucceeds(t *testing.T) {
	var n int32
	srv, _ := stub(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		switch atomic.AddInt32(&n, 1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(529)
		default:
			_, _ = io.WriteString(w, `{"model":"m","answers":{"q":{"type":"noul","noul":0.9}}}`)
		}
	})
	c := &Client{Key: func() string { return testKey }, BaseURL: srv.URL, Backoff: []time.Duration{0, 0}}
	res, err := c.Evaluate(context.Background(), Request{State: "s", Questions: map[string]Question{"q": Noul("?", "", "")}})
	if err != nil || *res.Answers["q"].Noul != 0.9 || n != 3 {
		t.Fatalf("res=%v err=%v calls=%d", res, err, n)
	}
	// out of retries → the 429 surfaces
	atomic.StoreInt32(&n, 0)
	c.Backoff = []time.Duration{0}
	if _, err := c.Evaluate(context.Background(), Request{State: "s", Questions: map[string]Question{"q": Noul("?", "", "")}}); err == nil {
		t.Fatal("want an error after exhausting retries")
	}
}

func TestTypeSafeRejectsUntypedOrForeignAnswers(t *testing.T) {
	for name, body := range map[string]string{
		"missing":      `{"model":"m","answers":{"comp":{"type":"noul","noul":0.1},"risk":{"type":"score","score":1}}}`,
		"wrong type":   `{"model":"m","answers":{"tier":{"type":"noul","noul":0.1},"comp":{"type":"noul","noul":0.1},"risk":{"type":"score","score":1}}}`,
		"foreign pick": `{"model":"m","answers":{"tier":{"type":"choice","choice":"public"},"comp":{"type":"noul","noul":0.1},"risk":{"type":"score","score":1}}}`,
		"no noul":      `{"model":"m","answers":{"tier":{"type":"choice","choice":"held"},"comp":{"type":"noul"},"risk":{"type":"score","score":1}}}`,
	} {
		body := body
		srv, _ := stub(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) { _, _ = io.WriteString(w, body) })
		c := &Client{Key: func() string { return testKey }, BaseURL: srv.URL}
		if _, err := c.Evaluate(context.Background(), tierReq()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTypeSafeConfigFromEnv(t *testing.T) {
	t.Setenv(EnvKey, " "+testKey+" ")
	t.Setenv(EnvURL, "http://example.invalid")
	t.Setenv(EnvModel, "jev-1.13")
	c := NewFromEnv()
	if c.Key() != testKey || c.BaseURL != "http://example.invalid" || c.ModelName() != "jev-1.13" || !c.Enabled() {
		t.Errorf("client = %+v", c)
	}
	t.Setenv(EnvModel, "")
	if NewFromEnv().ModelName() != DefaultModel {
		t.Error("default model")
	}
}

func TestTypeSafeFakeValidatesLikeTheClient(t *testing.T) {
	f := &Fake{Answers: map[string]Answer{"tier": ChoiceAnswer("secret", 1, nil)}}
	if _, err := f.Evaluate(context.Background(), Request{Questions: map[string]Question{"tier": Choice("?", map[string]any{"held": nil})}}); err == nil {
		t.Error("fake accepted an option that was not asked")
	}
	if len(f.Requests) != 1 {
		t.Errorf("fake recorded %d requests", len(f.Requests))
	}
}
