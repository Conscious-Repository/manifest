package server

import "testing"

func TestChatCodingModelCatalog(t *testing.T) {
	catalog := chatCodingModels([]byte(`{"models":[{"slug":"visible","display_name":"Visible model","visibility":"list"},{"slug":"secret","visibility":"hidden"},{"slug":"visible","visibility":"list"}]}`))
	models := catalog["codex"].Models
	if models[0].ID != "visible" || models[0].Label != "Visible model" {
		t.Fatalf("missing installed model: %+v", models)
	}
	seen := map[string]bool{}
	for _, m := range models {
		if m.ID == "secret" || seen[m.ID] {
			t.Fatalf("hidden or duplicate model: %+v", models)
		}
		seen[m.ID] = true
	}
	if !seen[catalog["codex"].Default] {
		t.Fatal("default must be selectable")
	}
	fallback := chatCodingModels([]byte("invalid"))
	if len(fallback["codex"].Models) == 0 || len(fallback["claude"].Models) != 3 {
		t.Fatal("configured fallback missing")
	}
}
