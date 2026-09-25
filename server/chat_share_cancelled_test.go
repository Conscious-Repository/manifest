package server

import (
	"context"
	"encoding/json"
	"manifest/agentchat"
	"manifest/artifacts"
	"strings"
	"testing"
)

func TestSharingExcludesCancelledInstructionContext(t *testing.T) {
	for _, agent := range []string{"kairos-private", "zeck-private"} {
		t.Run(agent, func(t *testing.T) {
			f := publicationFixture(t, agent)
			registry, err := artifacts.NewRegistry(f.s.artifacts)
			if err != nil {
				t.Fatal(err)
			}
			f.s.artifactReg = registry
			private, err := registry.Put(artifacts.Put{Kind: artifacts.KindFile, Title: "Never sent", Ref: "private-cancelled.txt", Content: []byte("CANCELLED_PRIVATE_ARTIFACT")})
			if err != nil {
				t.Fatal(err)
			}
			refs := []agentchat.ArtifactReference{{ID: private.Artifact.ID, Revision: private.Revision.Hash}, {ID: "missing-private-artifact", Revision: strings.Repeat("f", 64)}}
			store := f.s.agentChat.store
			if _, err := store.Accept(agent, f.id, "cancelled-share-request", "CANCELLED_PRIVATE_INSTRUCTION", &agentchat.MessageContext{Agent: agent, Artifacts: refs}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.CancelQueued(agent, f.id, "cancelled-share-request"); err != nil {
				t.Fatal(err)
			}
			source, body, _, _ := store.Get(agent, f.id)
			f.review = f.s.chatShareReview(context.Background(), source, body, f.s.codingContinuations(context.Background(), source))
			if len(f.review.Blockers) != 0 {
				t.Fatal("cancelled context blocked sharing", f.review.Blockers)
			}
			for _, file := range f.review.Files {
				if file.Hash == private.Revision.Hash {
					t.Fatal("cancelled artifact entered sharing manifest")
				}
			}
			result, err := f.publish(chatShareRequest{"cancelled-share-publish", f.review.Revision})
			if err != nil || result.State != "shared" {
				t.Fatal(result, err)
			}
			ag, _ := f.s.portalChatAgent(f.review.TargetAgent)
			threads := ag.Store.Threads()
			if len(threads) != 1 {
				t.Fatal(threads)
			}
			messages := ag.Store.Messages(threads[0].ID)
			raw, err := json.Marshal(messages)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "CANCELLED_PRIVATE") || strings.Contains(string(raw), private.Artifact.ID) || strings.Contains(string(raw), private.Revision.Hash) {
				t.Fatal("cancelled input entered team history", string(raw))
			}
			if f.s.artifacts.Owns(ag.Domain, private.Revision.Hash) {
				t.Fatal("sharing granted cancelled file access")
			}
			if !f.s.artifacts.Owns(ag.Domain, f.file.Hash) {
				t.Fatal("reviewed history attachment missing")
			}
			for _, file := range f.s.sharedConversationFiles(ag, threads[0].ID, f.review) {
				if file.Hash == private.Revision.Hash {
					t.Fatal("cancelled file entered team picker")
				}
			}
			if _, _, err := f.s.sharedSelectedFiles(ag, threads[0].ID, f.review, []string{private.Revision.Hash}); err == nil {
				t.Fatal("team input selected cancelled context")
			}
			retained, ok := store.Receipt(agent, f.id, "cancelled-share-request")
			if !ok || retained.State != agentchat.DeliveryCancelled || retained.Text != "CANCELLED_PRIVATE_INSTRUCTION" || len(retained.Context.Artifacts) != 2 {
				t.Fatal("private cancellation record changed", retained)
			}
		})
	}
}
