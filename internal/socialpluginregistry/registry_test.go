package socialpluginregistry_test

import (
	"testing"

	_ "qorvexus/internal/socialpluginautoload"
	"qorvexus/internal/socialpluginregistry"
)

func TestBuiltinsAreRegistered(t *testing.T) {
	names := socialpluginregistry.Names()
	expected := []string{"discord", "qqbot", "slack", "telegram"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d registered plugins, got %#v", len(expected), names)
	}
	for i, name := range expected {
		if names[i] != name {
			t.Fatalf("expected registered plugins %#v, got %#v", expected, names)
		}
	}
}
