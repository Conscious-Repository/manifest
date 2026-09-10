package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/chatthreads"
	"net/http"
	"os"
	"time"
)

type chatShareRequest struct {
	RequestID string `json:"requestId"`
	Revision  string `json:"revision"`
}
type chatShareResult struct {
	State        string                 `json:"state"`
	RequestID    string                 `json:"requestId"`
	Revision     string                 `json:"revision"`
	Conversation conversationDescriptor `json:"conversation"`
}

func shareResult(source agentchat.Session, review chatShareReview) chatShareResult {
	p := source.Sharing
	domain := "aion"
	if p.Agent == "zeck" {
		domain = "ooda"
	}
	return chatShareResult{p.State, p.RequestID, review.Revision, agentConversation("portal", p.Agent, p.Thread, "team:"+domain, "")}
}

// Recovery always decodes the saved approved envelope. Native histories and
// file metadata are never re-reviewed into a different payload after fencing.
func (s *Server) storedChatShare(agent, id string) (agentchat.Session, chatShareReview, error) {
	source, raw, err := s.agentChat.store.ReviewedShare(agent, id)
	if err != nil {
		return source, chatShareReview{}, err
	}
	var review chatShareReview
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if !validStoredShareReview(raw) || decoder.Decode(&review) != nil || review.Session.Agent != agent || review.Session.ID != id || review.SourceRevision != source.Sharing.Revision || review.TargetAgent != source.Sharing.Agent {
		return source, review, errors.New("sharing recovery record is invalid")
	}
	return source, review, nil
}

func (s *Server) publishChatShare(r *http.Request, agent, id string, request chatShareRequest) (chatShareResult, error) {
	if !privateShareSource(agent, id) || !agentchat.ValidRequestID(request.RequestID) || !artifacts.ValidHash(request.Revision) {
		return chatShareResult{}, errBadRequest("invalid sharing confirmation")
	}
	// Completed receipts can be recovered even while the now-shared runtime is
	// active. No publication writes happen on this path.
	if source, _, _, ok := s.agentChat.store.Get(agent, id); ok && source.Sharing != nil && source.Sharing.State == "shared" {
		source, review, err := s.storedChatShare(agent, id)
		if err != nil {
			return chatShareResult{}, err
		}
		if source.Sharing.RequestID != request.RequestID || review.Revision != request.Revision {
			return chatShareResult{}, agentchat.ErrRequestConflict
		}
		return shareResult(source, review), nil
	}
	mu := s.chatShareMutex(agent, id)
	if !mu.TryLock() {
		return chatShareResult{}, fmt.Errorf("%w: close this conversation's terminal view or wait for its current input to finish, then retry", agentchat.ErrShareBusy)
	}
	defer mu.Unlock()
	source, body, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		return chatShareResult{}, os.ErrNotExist
	}
	var review chatShareReview
	if source.Sharing != nil {
		var err error
		source, review, err = s.storedChatShare(agent, id)
		if err != nil {
			return chatShareResult{}, err
		}
		if source.Sharing.RequestID != request.RequestID || review.Revision != request.Revision {
			return chatShareResult{}, agentchat.ErrRequestConflict
		}
	} else {
		review = s.chatShareReview(r.Context(), source, body, s.codingContinuations(r.Context(), source))
		if len(review.Blockers) > 0 {
			return chatShareResult{}, fmt.Errorf("%w: %s", agentchat.ErrShareBusy, review.Blockers[0])
		}
		if review.Revision != request.Revision {
			return chatShareResult{}, agentchat.ErrShareChanged
		}
		thread := "shared-" + artifacts.Hash([]byte(agent + "/" + id))[:24]
		// Validate conversion before fencing. The full original envelope and source
		// revision must be representable without truncation or invented authors.
		checkedThread, checkedMessages, err := buildChatShareImport(review, thread)
		if err != nil {
			return chatShareResult{}, err
		}
		if err = chatthreads.ValidateSharedImport(checkedThread, checkedMessages); err != nil {
			return chatShareResult{}, err
		}
		payload, err := json.Marshal(review)
		if err != nil {
			return chatShareResult{}, err
		}
		source, _, err = s.agentChat.store.BeginReviewedShare(agent, id, request.RequestID, review.SourceRevision, review.TargetAgent, thread, payload)
		if err != nil {
			return chatShareResult{}, err
		}
	}
	result := shareResult(source, review)
	if source.Sharing.State == "shared" {
		return result, nil
	}
	ag, _ := s.portalChatAgent(review.TargetAgent)
	if ag == nil || ag.Store == nil {
		return result, errors.New("destination team chat is unavailable; retry the same share after it is restored")
	}
	thread, messages, err := buildChatShareImport(review, source.Sharing.Thread)
	if err != nil {
		return result, err
	}
	if err = s.stageChatShareFiles(ag, thread.ID, review); err != nil {
		return result, err
	}
	if _, err = ag.Store.ImportSharedThread(thread, messages, thread.Created); err != nil {
		return result, err
	}
	target, exists := portalChatThread(ag, thread.ID)
	if !exists || target.ImportRevision != review.Revision || target.ImportSource != thread.ImportSource || target.ImportFingerprint == "" || target.SharedSource == nil || *target.SharedSource != *thread.SharedSource {
		return result, errors.New("shared history could not be verified; retry this same share")
	}
	source, err = s.agentChat.store.CompleteShare(agent, id, request.RequestID, review.SourceRevision)
	if err != nil {
		return result, err
	}
	return shareResult(source, review), nil
}

func (s *Server) stageChatShareFiles(ag *chatAgent, thread string, review chatShareReview) error {
	for _, file := range review.Files {
		if s.artifacts == nil {
			return errors.New("attachment pool unavailable; sharing remains recoverable")
		}
		path := ""
		if file.ArtifactID != "" {
			path = s.artifacts.BlobPath(file.Hash)
		} else if s.threads != nil && s.threads.private != nil {
			path = s.threads.private.BlobPath(file.Hash)
		}
		data, err := readShareFile(path)
		if err != nil || int64(len(data)) != file.Size || artifacts.Hash(data) != file.Hash {
			return fmt.Errorf("approved attachment %q is missing or changed; restore it and retry the same share", file.Name)
		}
		ref, err := s.artifacts.Save(bytes.NewReader(data), file.Name, http.DetectContentType(data))
		if err != nil {
			return err
		}
		if ref.Hash != file.Hash {
			return errors.New("approved attachment hash changed")
		}
		at, _ := time.Parse(time.RFC3339Nano, review.Session.Created)
		if err = s.artifacts.Add(ag.Domain, artifacts.Entry{Ref: ref, By: review.OwnerName, ByEmail: review.OwnerEmail, At: at, Thread: thread}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleChatSharePublish(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	var request chatShareRequest
	if err := decode(r, &request); err != nil {
		httpError(w, err)
		return
	}
	result, err := s.publishChatShare(r, r.PathValue("agent"), r.PathValue("id"), request)
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		if result.State != "" {
			w.WriteHeader(status)
			writeJSON(w, map[string]any{"share": result, "error": err.Error()})
		} else {
			http.Error(w, err.Error(), status)
		}
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleChatShareStatus(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	if !privateShareSource(agent, id) {
		http.Error(w, "not a private team-agent conversation", http.StatusBadRequest)
		return
	}
	source, _, _, ok := s.agentChat.store.Get(agent, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if source.Sharing == nil {
		writeJSON(w, map[string]any{"state": "private"})
		return
	}
	source, review, err := s.storedChatShare(agent, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, shareResult(source, review))
}
