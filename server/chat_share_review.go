package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"manifest/agentchat"
	"manifest/artifacts"
	"net/http"
	"os"
	"sort"
)

// This is a review, not publication. Reading it must not grant attachment
// access or create a team thread. The eventual commit must validate Revision
// AND fence every included writer before using this envelope.
type chatShareReview struct {
	Revision       string                                     `json:"revision"`
	SourceRevision string                                     `json:"sourceRevision"`
	Audience       string                                     `json:"audience"`
	TargetAgent    string                                     `json:"targetAgent"`
	FutureMessages bool                                       `json:"futureMessages"`
	Session        agentchat.Session                          `json:"session"`
	Body           string                                     `json:"body"`
	Timeline       []conversationTimelineTurn                 `json:"timeline"`
	Continuations  []codingContinuationView                   `json:"continuations"`
	NativeReceipts map[string]map[string]terminalInputReceipt `json:"nativeReceipts"`
	Files          []chatShareFile                            `json:"files"`
	Operations     []map[string]any                           `json:"operations"`
	Proposals      []approvalRow                              `json:"proposals"`
	CodingResults  []chatCodingResult                         `json:"codingResults"`
	PlanRevisions  []chatPlanRevision                         `json:"planRevisions"`
	Blockers       []string                                   `json:"blockers"`
}

type chatShareFile struct {
	ArtifactID string   `json:"artifactId,omitempty"`
	Hash       string   `json:"hash"`
	Name       string   `json:"name"`
	Size       int64    `json:"size"`
	References []string `json:"references"`
}

func (s *Server) handleChatShareReview(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	if agent != "kairos-private" && agent != "zeck-private" {
		http.Error(w, "sharing review requires a private Kairos or Zeck conversation", http.StatusBadRequest)
		return
	}
	sess, body, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	review := s.chatShareReview(r.Context(), sess, body, s.codingContinuations(r.Context(), sess))
	// A root writer may have completed while files/native history were read.
	// Do not offer a review assembled around an already superseded source.
	current, currentBody, _, exists := s.agentChat.store.Get(agent, id)
	if !exists || agentchat.ShareRevision(current, currentBody) != review.SourceRevision {
		http.Error(w, "conversation changed while preparing sharing review; review again", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, review)
}

func (s *Server) chatShareReview(ctx context.Context, sess agentchat.Session, body string, views []codingContinuationView) chatShareReview {
	target, audience := "kairos", "AION team"
	if sess.Agent == "zeck-private" {
		target, audience = "zeck", "OODA team"
	}
	r := chatShareReview{
		SourceRevision: agentchat.ShareRevision(sess, body), Audience: audience,
		TargetAgent: target, FutureMessages: true, Session: sess, Body: body,
		Timeline: conversationTimeline(sess, body, views), Continuations: views,
		Files: []chatShareFile{}, Blockers: []string{},
		NativeReceipts: map[string]map[string]terminalInputReceipt{},
		Operations:     s.chatOperations(sess.ID), Proposals: s.chatTaskProposals(sess),
		CodingResults: s.chatCodingResults(sess), PlanRevisions: s.chatPlanRevisions(sess, body),
	}
	blocked := map[string]bool{}
	block := func(message string) {
		if !blocked[message] {
			blocked[message] = true
			r.Blockers = append(r.Blockers, message)
		}
	}
	if ag, _ := s.portalChatAgent(target); ag == nil {
		block("The destination team chat is not configured.")
	}
	if sess.Sharing != nil {
		block("Sharing has already started; recover the existing share instead of starting another.")
	}
	if sess.Origin != nil && sess.Origin.Mode == "continue" {
		block("Review sharing from the original conversation so its earlier history is included.")
	}
	if sess.Status != agentchat.StatusIdle {
		block("Wait for the current agent turn to finish.")
	}
	for _, d := range sess.Deliveries {
		if d.State == agentchat.DeliveryQueued || d.State == agentchat.DeliveryRunning || d.State == agentchat.DeliveryInterrupted {
			block("Finish or reconcile pending conversation deliveries before sharing.")
		}
	}
	// Running terminals can write outside Manifest's send path. An idle prompt
	// is insufficient evidence that their transcript is immutable.
	for _, v := range views {
		if !v.HistoryAvailable {
			block("Terminal continuation " + v.ID + " history could not be read; review again when it is available.")
		}
		if v.Process != "stopped" && v.Process != "not-started" {
			block("Stop and verify terminal continuation " + v.ID + " before sharing its history.")
		}
	}
	files := map[string]int{}
	add := func(file chatShareFile, reference string) {
		key := file.ArtifactID + ":" + file.Hash + ":" + file.Name
		if i, ok := files[key]; ok {
			for _, existing := range r.Files[i].References {
				if existing == reference {
					return
				}
			}
			r.Files[i].References = append(r.Files[i].References, reference)
			return
		}
		files[key] = len(r.Files)
		file.References = []string{reference}
		r.Files = append(r.Files, file)
	}
	artifact := func(ref agentchat.ArtifactReference, source string) {
		if s.artifactReg == nil || s.artifacts == nil {
			block("An attached artifact cannot be read: " + ref.ID)
			return
		}
		a, ok := s.artifactReg.Get(ref.ID)
		if !ok {
			block("An attached artifact is missing: " + ref.ID)
			return
		}
		version, ok := a.Revision(ref.Revision)
		if !ok {
			block("An attached artifact version is missing: " + ref.ID)
			return
		}
		// Read the immutable pool, never a.Ref or HeadRevision. Limit reads
		// before allocation; the sharing route must not load arbitrary-size files.
		path := s.artifacts.BlobPath(version.Hash)
		data, err := readShareFile(path)
		if err != nil || artifacts.Hash(data) != ref.Revision {
			block("An attached artifact version is unreadable or changed: " + ref.ID)
			return
		}
		add(chatShareFile{ArtifactID: ref.ID, Hash: ref.Revision, Name: version.Ref, Size: int64(len(data))}, source)
	}
	if sess.Origin != nil {
		for _, ref := range sess.Origin.Artifacts {
			artifact(ref, "origin")
		}
	}
	for _, d := range sess.Deliveries {
		if d.Context != nil {
			for _, ref := range d.Context.Artifacts {
				artifact(ref, "delivery:"+d.ID)
			}
		}
	}
	for _, v := range views {
		// Include unmatched receipts too: an uncertain native send can contain
		// selected files even before a transcript turn is available.
		receipts := v.Submissions
		if s.terminal != nil {
			receipts = s.terminal.continuationReceipts(v.ID, sessionConversation(sess).Key)
		}
		r.NativeReceipts[v.ID] = receipts
		for key, receipt := range receipts {
			if receipt.State != "sent" {
				block("Reconcile the uncertain terminal delivery " + receipt.ID + " before sharing.")
			}
			for _, ref := range receipt.Artifacts {
				artifact(ref, "terminal:"+v.ID+":"+key)
			}
		}
		for key, proposal := range v.PlanRevisions {
			artifact(agentchat.ArtifactReference{ID: proposal.ArtifactID, Revision: proposal.BaseRevision}, "terminal-plan:"+v.ID+":"+key)
		}
	}
	for _, turn := range r.Timeline {
		for _, match := range fileTokenRe.FindAllStringSubmatch(turn.Text, -1) {
			path := ""
			if s.threads != nil && s.threads.private != nil {
				path = s.threads.private.BlobPath(match[1])
			}
			data, err := readShareFile(path)
			if err != nil || artifacts.Hash(data) != match[1] {
				block("An uploaded file is missing or changed: " + match[2])
				continue
			}
			add(chatShareFile{Hash: match[1], Name: match[2], Size: int64(len(data))}, fmt.Sprintf("turn:%v", turn.N))
		}
	}
	// Map traversal order must not change the reviewed approval fingerprint.
	for i := range r.Files {
		sort.Strings(r.Files[i].References)
	}
	sort.Slice(r.Files, func(i, j int) bool {
		a, b := r.Files[i], r.Files[j]
		return a.ArtifactID+":"+a.Hash+":"+a.Name < b.ArtifactID+":"+b.Hash+":"+b.Name
	})
	sort.Strings(r.Blockers)
	if ctx.Err() != nil {
		block("Sharing review was interrupted; review again.")
	}
	encoded, _ := json.Marshal(r)
	r.Revision = artifacts.Hash(encoded)
	return r
}

func readShareFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, artifacts.MaxBlobSize+1))
	if int64(len(b)) > artifacts.MaxBlobSize {
		return nil, fmt.Errorf("attachment exceeds sharing limit")
	}
	return b, err
}
