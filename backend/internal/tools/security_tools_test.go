package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSplitCommandLine(t *testing.T) {
	parts, err := splitCommandLine(`--header "X-Test: value" --threads 10 --flag 'two words'`)
	if err != nil {
		t.Fatalf("splitCommandLine returned error: %v", err)
	}

	want := []string{"--header", "X-Test: value", "--threads", "10", "--flag", "two words"}
	if len(parts) != len(want) {
		t.Fatalf("unexpected arg count: got %d want %d (%v)", len(parts), len(want), parts)
	}

	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("unexpected arg at %d: got %q want %q", i, parts[i], want[i])
		}
	}
}

func TestBuildNmapCommand(t *testing.T) {
	raw, err := json.Marshal(NmapArgs{
		Target:   "example.com",
		Ports:    "80,443",
		ScanType: "version",
		Flags:    "--script banner",
		Timeout:  Int64(90),
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	spec, err := buildNmapCommand(raw)
	if err != nil {
		t.Fatalf("buildNmapCommand returned error: %v", err)
	}

	if spec.binary != "nmap" {
		t.Fatalf("unexpected binary: %q", spec.binary)
	}
	if spec.timeout != 90*time.Second {
		t.Fatalf("unexpected timeout: %s", spec.timeout)
	}

	rendered := renderCommand(spec.binary, spec.args)
	for _, needle := range []string{"nmap", "-sV", "-p", "80,443", "--script", "banner", "example.com"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("expected %q in rendered command %q", needle, rendered)
		}
	}
}

func TestExtractNmapHighlights(t *testing.T) {
	output := strings.Join([]string{
		"Host is up (0.020s latency).",
		"80/tcp open http Apache httpd 2.4.57",
		"443/tcp open ssl/http nginx 1.24.0",
	}, "\n")

	highlights := extractNmapHighlights(output)
	if len(highlights) < 2 {
		t.Fatalf("expected highlights, got %v", highlights)
	}

	if !strings.Contains(strings.Join(highlights, "\n"), "Open port 80/tcp") {
		t.Fatalf("expected port summary in highlights: %v", highlights)
	}
}

func TestNewToolRegistryIncludesPhaseSixTools(t *testing.T) {
	reg := NewToolRegistry(nil, nil, nil, uuid.UUID{}, nil, nil, nil, nil)
	defs := reg.Available()

	want := map[string]bool{
		"nmap":         false,
		"sqlmap":       false,
		"theharvester": false,
		"wfuzz":        false,
		"done":         false,
	}

	for _, def := range defs {
		if _, ok := want[def.Name]; ok {
			want[def.Name] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Fatalf("expected tool %q to be registered", name)
		}
	}
}
