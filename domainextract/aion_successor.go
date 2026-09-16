package domainextract

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"manifest/hermes"
)

// AionSuccessorConfig is a separate readiness contract, not execution authority.
// Zero capacities mean unknown, not unlimited. No provider capacity has yet
// been verified for this deployment; declarations cannot certify themselves.
type AionSuccessorConfig struct {
	Provider            string   `json:"provider"`
	Model               string   `json:"model"`
	Endpoint            string   `json:"endpoint"`
	ProviderBinding     string   `json:"providerBinding"`
	Tools               []string `json:"tools"`
	MCP                 string   `json:"mcp"`
	Fallback            bool     `json:"fallback"`
	CostTelemetry       string   `json:"costTelemetry"`
	InputCapacityBytes  int      `json:"inputCapacityBytes"`
	ContextWindowTokens int      `json:"contextWindowTokens"`
}

func DecodeAionSuccessorConfig(raw []byte) (AionSuccessorConfig, error) {
	var c AionSuccessorConfig
	if len(raw) > 8192 || strict(raw, &c) != nil {
		return c, errors.New("invalid-successor-config")
	}
	return c, c.validate()
}
func (c AionSuccessorConfig) validate() error {
	if c.Provider != "lab-sparks" || c.Model != "deepseek-v4.1-flash" || c.Endpoint != hermes.LocalEndpoint || c.ProviderBinding != hermes.LocalProviderBinding {
		return errors.New("provider-binding-mismatch")
	}
	if len(c.Tools) != 1 || c.Tools[0] != "none" || c.MCP != "no_mcp" || c.Fallback || c.CostTelemetry != "unavailable" {
		return errors.New("provider-scope-mismatch")
	}
	if c.InputCapacityBytes < 0 || c.ContextWindowTokens < 0 {
		return errors.New("invalid-provider-capacity")
	}
	return nil
}

// AionInput holds private immutable-by-API bytes. It is never a job, proposal,
// approval, or execution receipt. The closed namespace facts are exactly those
// of ReadContextManifest: AION requires three files and no directory namespace.
type AionInput struct {
	manifest   ContextManifest
	input      Input
	serialized []byte
	digest     string
}

const maxAionSourceBytes = 8 << 20

func readAionInput(root string, names []string) (AionInput, error) {
	var s AionInput
	if len(names) == 0 || len(names) > 4 {
		return s, errors.New("invalid-source-selection")
	}
	m, err := readContextManifest(root, "aion", maxAionSourceBytes)
	if err != nil {
		return s, errors.New("context-read-unavailable")
	}
	in := Input{Ritual: "aion", Context: m.context}
	seen := map[string]bool{}
	for _, name := range names {
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\\n\r\x00") || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, "system/") || strings.HasPrefix(name, "extrinsic/") || seen[name] {
			return s, errors.New("invalid-source-selection")
		}
		seen[name] = true
		f, e := openPlanFile(filepath.Join(root, name), false)
		if e != nil {
			return s, errors.New("source-read-unavailable")
		}
		b, e := io.ReadAll(io.LimitReader(f, maxAionSourceBytes+1))
		f.Close()
		if e != nil || len(b) > maxAionSourceBytes || !utf8.Valid(b) || strings.TrimSpace(string(b)) == "" {
			return s, errors.New("invalid-source-bytes")
		}
		in.Documents = append(in.Documents, Document{Name: name, Text: string(b)})
	}
	for _, body := range m.context {
		if !utf8.ValidString(body) {
			return s, errors.New("invalid-context-bytes")
		}
	}
	// Include metadata/namespace facts AND every original byte, with JSON escaping.
	raw, err := json.Marshal(struct {
		Version  int             `json:"version"`
		Manifest ContextManifest `json:"manifest"`
		Input    Input           `json:"input"`
	}{1, m, in})
	if err != nil {
		return s, errors.New("input-serialization-unavailable")
	}
	return AionInput{m, in, raw, byteHash(raw)}, nil
}

// BuildAionInput reads twice to detect observed drift. This is not a filesystem
// transaction: copied immutable fixtures are preferred over a mutable live tree.
// expected pins a previously reviewed snapshot; an empty pin is measurement only.
func BuildAionInput(root string, names []string, expected string) (AionInput, error) {
	s, err := readAionInput(root, names)
	if err != nil {
		return AionInput{}, err
	}
	if expected != "" && expected != s.digest {
		return AionInput{}, errors.New("input-drift")
	}
	if err = s.Revalidate(root); err != nil {
		return AionInput{}, err
	}
	return s, nil
}
func (s AionInput) Revalidate(root string) error {
	names := []string{}
	for _, d := range s.input.Documents {
		names = append(names, d.Name)
	}
	current, err := readAionInput(root, names)
	if err != nil || current.digest != s.digest {
		return errors.New("input-drift")
	}
	return nil
}

type AionReadinessReport struct {
	Version              int    `json:"version"`
	State                string `json:"state"`
	Reason               string `json:"reason"`
	InputSHA256          string `json:"inputSha256,omitempty"`
	ManifestSHA256       string `json:"manifestSha256,omitempty"`
	SourcesSHA256        string `json:"sourcesSha256,omitempty"`
	ProviderConfigSHA256 string `json:"providerConfigSha256,omitempty"`
	SerializedBytes      int    `json:"serializedBytes"`
	LegacyInputBytes     int    `json:"legacyInputBytes"`
	RecordCount          int    `json:"recordCount"`
	SourceCount          int    `json:"sourceCount"`
	InvocationPerformed  bool   `json:"invocationPerformed"`
	RequestSHA256        string `json:"requestSha256,omitempty"`
	ResultSHA256         string `json:"resultSha256,omitempty"`
	UsageState           string `json:"usageState"`
	SemanticState        string `json:"semanticState"`
}

// Readiness always refuses. There is deliberately no runner, HTTP client,
// fallback, writer, handoff or approval store in this seam. Input hashes are not
// request hashes: no request/result/usage evidence exists without an invocation.
func (s AionInput) Readiness(c AionSuccessorConfig) AionReadinessReport {
	r := AionReadinessReport{Version: 1, State: "refused", Reason: "provider-capacity-unverified", UsageState: "not-invoked", SemanticState: "owner-review-required"}
	if err := c.validate(); err != nil {
		r.Reason = err.Error()
		return r
	}
	if s.digest == "" || byteHash(s.serialized) != s.digest {
		r.Reason = "invalid-input"
		return r
	}
	r.InputSHA256 = s.digest
	r.ManifestSHA256 = s.manifest.SHA256
	r.SourcesSHA256 = reducerDigest(s.input.Documents)
	r.ProviderConfigSHA256 = reducerDigest(c)
	r.SerializedBytes = len(s.serialized)
	raw, _ := json.Marshal(s.input)
	r.LegacyInputBytes = len(raw)
	r.RecordCount = len(s.manifest.Records)
	r.SourceCount = len(s.input.Documents)
	if c.InputCapacityBytes > 0 && r.SerializedBytes > c.InputCapacityBytes {
		r.Reason = "complete-input-exceeds-declared-capacity"
	}
	return r
}

// CheckCopiedReply performs only offline strict shape/evidence validation. It
// cannot turn supplied bytes into a provider receipt or authorize any action.
func (s AionInput) CheckCopiedReply(c AionSuccessorConfig, raw []byte) (digest string, candidates int, err error) {
	if s.digest == "" || byteHash(s.serialized) != s.digest {
		return "", 0, errors.New("invalid-input")
	}
	if e := c.validate(); e != nil {
		return "", 0, e
	}
	digest = reducerDigest(struct{ Input, ProviderConfig, Result string }{s.digest, reducerDigest(c), byteHash(raw)})
	if !utf8.Valid(raw) {
		return digest, 0, errors.New("candidate-contract-refused")
	}
	ps, e := validateReplyEvidence(s.input, string(raw))
	if e != nil {
		return digest, 0, errors.New("candidate-contract-refused")
	}
	return digest, len(ps), nil
}
