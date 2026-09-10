package server

import (
	"os/exec"
	"testing"
)

func TestArtifactRevisionDiff(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/artifact-diff.cjs").CombinedOutput(); err != nil {
		t.Fatalf("artifact diff: %v\n%s", err, out)
	}
}

func TestChatPinsUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-pins.cjs").CombinedOutput(); err != nil {
		t.Fatalf("chat pins: %v\n%s", err, out)
	}
}

func TestChatDeliveryRecoveryUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-delivery.cjs").CombinedOutput(); err != nil {
		t.Fatalf("delivery UI: %v\n%s", err, out)
	}
}

func TestChatDraftRecoveryUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-state.cjs").CombinedOutput(); err != nil {
		t.Fatalf("draft UI: %v\n%s", err, out)
	}
}

func TestChatArtifactLoadNavigationRace(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-load-race.cjs").CombinedOutput(); err != nil {
		t.Fatalf("artifact load: %v\n%s", err, out)
	}
}

func TestChatReadingPositionUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-reading.cjs").CombinedOutput(); err != nil {
		t.Fatalf("reading position: %v\n%s", err, out)
	}
}

func TestChatUploadNavigationRace(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-upload-race.cjs").CombinedOutput(); err != nil {
		t.Fatalf("upload navigation: %v\n%s", err, out)
	}
}

func TestChatLandingDraftUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-landing.cjs").CombinedOutput(); err != nil {
		t.Fatalf("landing draft: %v\n%s", err, out)
	}
}

func TestChatDeliveryAcrossDevices(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/chat-delivery-devices.cjs").CombinedOutput(); err != nil {
		t.Fatalf("cross-device delivery: %v\n%s", err, out)
	}
}
