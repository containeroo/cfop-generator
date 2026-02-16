package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseZonefileParsesAndSortsDeterministically(t *testing.T) {
	input := `
; comment
$ORIGIN example.com.
b.example.com. 1 IN A 192.0.2.2
example.com. 300 IN NS ns1.example.com.
a.example.com. 1 IN A 192.0.2.1
`

	records, err := parseZonefile([]byte(input), true)
	if err != nil {
		t.Fatalf("parseZonefile returned error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0].Name != "a.example.com" || records[1].Name != "b.example.com" {
		t.Fatalf("records not sorted deterministically: %+v", records)
	}
	if !records[0].Proxied || !records[1].Proxied {
		t.Fatalf("expected proxied=true on parsed records: %+v", records)
	}
}

func TestParseZonefileRejectsInvalidFormat(t *testing.T) {
	_, err := parseZonefile([]byte("invalid-line"), true)
	if err == nil {
		t.Fatal("expected error for invalid zonefile line")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line number in error, got %q", err.Error())
	}
}

func TestParseZonefileRejectsInvalidSRVAndMX(t *testing.T) {
	_, err := parseZonefile([]byte("_svc.example.com. 1 IN SRV 1 2 3\n"), true)
	if err == nil || !strings.Contains(err.Error(), "SRV") {
		t.Fatalf("expected SRV validation error, got %v", err)
	}

	_, err = parseZonefile([]byte("example.com. 1 IN MX 10\n"), true)
	if err == nil || !strings.Contains(err.Error(), "MX") {
		t.Fatalf("expected MX validation error, got %v", err)
	}
}

func TestRunCompletionBash(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"completion", "bash"}, &stdout, &stderr, cliName)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "complete -F _cfop_generator_completion cfop-generator") {
		t.Fatalf("unexpected completion output: %s", stdout.String())
	}
}

func TestRunCompletionUnsupportedShell(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"completion", "fish"}, &stdout, &stderr, cliName)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unsupported shell") {
		t.Fatalf("expected unsupported shell error, got %q", stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"version"}, &stdout, &stderr, cliName)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("expected version %q, got %q", version, strings.TrimSpace(stdout.String()))
	}
}

func TestRunConvertWritesOutputWithSecurePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	zonePath := filepath.Join(tmpDir, "zone.txt")
	outputPath := filepath.Join(tmpDir, "out.yaml")

	zoneContent := "example.com. 1 IN A 192.0.2.1\n"
	if err := os.WriteFile(zonePath, []byte(zoneContent), 0o644); err != nil {
		t.Fatalf("failed to create zonefile: %v", err)
	}
	if err := os.WriteFile(outputPath, []byte("old"), 0o666); err != nil {
		t.Fatalf("failed to create output file: %v", err)
	}

	var stderr bytes.Buffer
	exitCode := run([]string{"-file", zonePath, "-output", outputPath}, io.Discard, &stderr, cliName)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", exitCode, stderr.String())
	}

	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed reading output file: %v", err)
	}
	if !strings.Contains(string(outputBytes), "kind: DNSRecord") {
		t.Fatalf("unexpected rendered output: %s", string(outputBytes))
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("failed to stat output file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected output mode 0600, got %o", info.Mode().Perm())
	}
}

func TestRunConvertFromFixture(t *testing.T) {
	zoneContent, err := os.ReadFile("testdata/example.com.txt")
	if err != nil {
		t.Fatalf("failed reading fixture: %v", err)
	}

	records, err := parseZonefile(zoneContent, false)
	if err != nil {
		t.Fatalf("parseZonefile returned error: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected parsed records from fixture")
	}
	if records[0].Proxied {
		t.Fatal("expected proxied=false")
	}

	var rendered bytes.Buffer
	if err := renderTemplate(&rendered, records); err != nil {
		t.Fatalf("renderTemplate returned error: %v", err)
	}

	out := rendered.String()
	if !strings.Contains(out, "type: A") || !strings.Contains(out, "type: MX") || !strings.Contains(out, "type: SRV") {
		t.Fatalf("rendered output missing expected record types: %s", out)
	}
}
