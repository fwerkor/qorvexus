package runtimecontrol

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClientRequestAndStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	client := NewClient(dir)
	if !client.Enabled() {
		t.Fatal("expected client to be enabled")
	}
	if err := client.RequestSwitchBinary("/tmp/qorvexus-next", "apply update"); err != nil {
		t.Fatal(err)
	}
	req, ok, err := LoadPendingRequest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pending request")
	}
	if req.Action != ActionSwitchBinary {
		t.Fatalf("expected %s, got %s", ActionSwitchBinary, req.Action)
	}
	if req.BinaryPath != "/tmp/qorvexus-next" {
		t.Fatalf("unexpected binary path %q", req.BinaryPath)
	}
	if err := ClearPendingRequest(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadPendingRequest(dir); err != nil || ok {
		t.Fatalf("expected request to be cleared, ok=%v err=%v", ok, err)
	}

	state := State{
		Mode:           "supervised",
		ChildPID:       42,
		BinaryPath:     filepath.Join(dir, "qorvexus"),
		SourceRoot:     filepath.Join(dir, "src"),
		ChildStartedAt: time.Now().UTC(),
		LastRestartAt:  time.Now().UTC(),
		LastRequest:    &req,
	}
	if err := WriteState(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ChildPID != state.ChildPID {
		t.Fatalf("expected child pid %d, got %d", state.ChildPID, loaded.ChildPID)
	}
	if loaded.LastRequest == nil || loaded.LastRequest.Action != ActionSwitchBinary {
		t.Fatalf("expected last request to round-trip, got %#v", loaded.LastRequest)
	}
}

func TestRequestRejectsInvalidAction(t *testing.T) {
	client := NewClient(t.TempDir())
	err := client.Request(Request{Action: "explode"})
	if err == nil {
		t.Fatal("expected invalid action error")
	}
}
