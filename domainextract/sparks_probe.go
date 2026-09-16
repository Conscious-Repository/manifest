package domainextract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/secrets"
)

// SparksProbe is deliberately not SparksAccounting: discovery cannot authorize
// a canary. Zero token counts and byte bound mean unverified, never unlimited.
// Only fixed labels, bindings, hashes and counts leave this seam.
type SparksProbe struct {
	Version                int    `json:"version"`
	Provider               string `json:"provider"`
	Endpoint               string `json:"endpoint"`
	Model                  string `json:"model"`
	InputSHA256            string `json:"inputSha256"`
	ConfigSHA256           string `json:"configSha256"`
	CapabilitySHA256       string `json:"capabilitySha256"`
	RequestSHA256          string `json:"requestSha256"`
	RequestBytes           int    `json:"requestBytes"`
	ModelsSHA256           string `json:"modelsSha256,omitempty"`
	EndpointEvidenceSHA256 string `json:"endpointEvidenceSha256,omitempty"`
	TokenizerSHA256        string `json:"tokenizerSha256,omitempty"`
	TemplateSHA256         string `json:"templateSha256,omitempty"`
	SerializedTokens       int    `json:"serializedTokens"`
	PromptTokens           int    `json:"promptTokens"`
	VerifiedRequestBytes   int    `json:"verifiedRequestBytes"`
	ReservedOutput         int    `json:"reservedOutput"`
	TokenMargin            int    `json:"tokenMargin"`
	ByteMargin             int    `json:"byteMargin"`
	State                  string `json:"state"`
	Reason                 string `json:"reason"`
	ProbeSHA256            string `json:"probeSha256"`
}

// ProbeSparksAccounting performs at most two fixed-origin GETs when liveRead is
// explicit. It never POSTs, tokenizes private text, or invokes completions. There
// is currently no supported authoritative accounting response contract on this
// deployment. New contracts require a reviewed adapter, not guessed JSON fields.
func (s AionInput) ProbeSparksAccounting(ctx context.Context, c AionSuccessorConfig, cap SparksCapability, request []byte, reserve int, liveRead bool) SparksProbe {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 << 10}
	defer transport.CloseIdleConnections()
	return s.probeSparks(ctx, c, cap, request, reserve, liveRead, transport, 10*time.Second)
}

func (s AionInput) probeSparks(ctx context.Context, c AionSuccessorConfig, cap SparksCapability, request []byte, reserve int, live bool, transport http.RoundTripper, timeout time.Duration) (r SparksProbe) {
	r = SparksProbe{Version: 1, State: "tokenizer-template-accounting-required", ReservedOutput: reserve, TokenMargin: 256, ByteMargin: 1024}
	defer func() { r.ProbeSHA256 = reducerDigest(r) }()
	fail := func(reason string) SparksProbe { r.Reason = reason; return r }
	expected, err := s.SparksRequest(c, reserve)
	if err != nil {
		return fail("invalid-request-input")
	}
	if len(request) > 16<<20 || !bytes.Equal(expected, request) {
		return fail("accounting-request-mismatch")
	}
	if cap.Provider != c.Provider || cap.Endpoint != c.Endpoint || cap.Model != c.Model || !evidenceHash.MatchString(cap.ModelsSHA256) || cap.MaxModelLen < 1 || cap.MaxModelLen > 16<<20 {
		return fail("provider-capacity-unverified")
	}
	r.Provider, r.Endpoint, r.Model = c.Provider, c.Endpoint, c.Model
	r.InputSHA256, r.ConfigSHA256, r.CapabilitySHA256 = s.digest, reducerDigest(c), reducerDigest(cap)
	r.RequestSHA256, r.RequestBytes = byteHash(request), len(request)
	if reserve < 4096 || reserve+r.TokenMargin >= cap.MaxModelLen {
		return fail("insufficient-output-reserve")
	}
	if !live {
		return fail("endpoint-inspection-required")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(endpoint string) ([]byte, error) {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if e != nil {
			return nil, errors.New("endpoint-refused")
		}
		resp, e := client.Do(req)
		if e != nil {
			return nil, errors.New("metadata-transport-failed-or-timeout")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("metadata-endpoint-refused")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if e != nil || len(b) > 1<<20 {
			return nil, errors.New("metadata-body-refused")
		}
		var checked json.RawMessage
		if !utf8.Valid(b) || len(secrets.Scan(string(b))) > 0 || strict(b, &checked) != nil {
			return nil, errors.New("metadata-content-refused")
		}
		return b, nil
	}
	models, e := get(c.Endpoint + "/models")
	if e != nil {
		return fail(e.Error())
	}
	actual, e := LoadSparksCapability(c, models)
	if e != nil || actual.Model != cap.Model || actual.MaxModelLen != cap.MaxModelLen {
		return fail("model-capability-mismatch")
	}
	r.ModelsSHA256 = byteHash(models)
	schema, e := get(strings.TrimSuffix(c.Endpoint, "/v1") + "/openapi.json")
	if e != nil {
		return fail(e.Error())
	}
	r.EndpointEvidenceSHA256 = byteHash(schema)
	// Inspect only the documented read-only tokenizer operation. OpenAPI permits
	// extension fields; strict above still rejects duplicate keys and bad JSON.
	var api struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema json.RawMessage `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if json.Unmarshal(schema, &api) != nil {
		return fail("metadata-schema-refused")
	}
	op, ok := api.Paths["/tokenize"]["post"]
	if !ok {
		return fail("tokenizer-endpoint-unsupported")
	}
	shape := op.Responses["200"].Content["application/json"].Schema
	var fields map[string]json.RawMessage
	if len(shape) == 0 || json.Unmarshal(shape, &fields) != nil || len(fields) == 0 {
		return fail("tokenizer-response-contract-undocumented")
	}
	// A model-window declaration, raw count, token IDs, or chat request schema
	// cannot prove deployed template identity or byte admission. Fail closed even
	// if a future server advertises fields that resemble an accounting response.
	return fail("tokenizer-template-contract-unsupported")
}
