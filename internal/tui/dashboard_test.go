package tui

import (
	"strings"
	"testing"
)

func TestViewFleetContainsAgentfltLogo(t *testing.T) {
	// Logo constant should be non-empty and contain box-drawing chars (╔ ╗ ║ ═ etc)
	if asciiLogoAgentflt == "" {
		t.Fatal("asciiLogoAgentflt must be non-empty")
	}
	if !strings.Contains(asciiLogoAgentflt, "██") && !strings.Contains(asciiLogoAgentflt, "╔") {
		t.Errorf("asciiLogoAgentflt should contain box-drawing chars (██ or ╔), got: %q", asciiLogoAgentflt)
	}
}
