/*
 * © 2025 Labmonkeys Space
 * Apache-2.0 — see LICENSE.
 */

package main

import (
	"math/rand"
	"strings"
	"testing"
)

// TestEmbeddedSyslogCatalogParses is the primary smoke test: the default
// catalog shipped via //go:embed must load with no errors and contain exactly
// the six universal entries listed in design.md §D10.
func TestEmbeddedSyslogCatalogParses(t *testing.T) {
	cat, err := LoadEmbeddedSyslogCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedSyslogCatalog: %v", err)
	}
	wantNames := []string{
		"interface-up", "interface-down",
		"auth-success", "auth-failure",
		"config-change", "system-restart",
	}
	if got, want := len(cat.Entries), len(wantNames); got != want {
		t.Fatalf("entry count: got %d, want %d", got, want)
	}
	for _, n := range wantNames {
		if _, ok := cat.ByName[n]; !ok {
			t.Errorf("entry %q missing from embedded catalog", n)
		}
	}
	// Total weight per design.md §D10 = 40 + 40 + 20 + 20 + 10 + 5 = 135.
	if got := cat.totalWeight; got != 135 {
		t.Errorf("totalWeight: got %d, want 135", got)
	}
}

// TestSyslogCatalogInvalidJSON ensures malformed JSON gives a clear error.
func TestSyslogCatalogInvalidJSON(t *testing.T) {
	_, err := parseSyslogCatalog([]byte(`{"entries": [`), "<test>")
	if err == nil {
		t.Fatal("parseSyslogCatalog: expected error on malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error text: got %q, want mention of parsing", err.Error())
	}
}

// TestSyslogCatalogOutOfRangeSeverity exercises both the integer-range
// check and the unknown-name check on severity.
func TestSyslogCatalogOutOfRangeSeverity(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "int too high",
			body: `{"entries":[{"name":"x","facility":"user","severity":8,"template":"m"}]}`,
			want: "out of range",
		},
		{
			name: "unknown string",
			body: `{"entries":[{"name":"x","facility":"user","severity":"notASeverity","template":"m"}]}`,
			want: "unknown severity name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSyslogCatalog([]byte(tc.body), "<test>")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q: want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestSyslogCatalogUnknownFacility covers the facility side of the enum check.
func TestSyslogCatalogUnknownFacility(t *testing.T) {
	body := `{"entries":[{"name":"x","facility":"notAFacility","severity":"info","template":"m"}]}`
	_, err := parseSyslogCatalog([]byte(body), "<test>")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown facility name") {
		t.Errorf("error %q: want 'unknown facility name'", err.Error())
	}
}

// TestSyslogCatalogUnknownTemplateField rejects {{.NotAField}} at load.
func TestSyslogCatalogUnknownTemplateField(t *testing.T) {
	body := `{"entries":[{"name":"x","facility":"user","severity":"info","template":"hi {{.NotAField}}"}]}`
	_, err := parseSyslogCatalog([]byte(body), "<test>")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown template field") || !strings.Contains(err.Error(), "NotAField") {
		t.Errorf("error %q: want mention of unknown template field and field name", err.Error())
	}
}

// TestSyslogCatalogOversizeRejected exercises the §D12 MTU-safety guard. A
// template that renders beyond maxSyslogMessageBytes must be rejected at
// load time rather than producing truncated wire output at fire time.
func TestSyslogCatalogOversizeRejected(t *testing.T) {
	big := strings.Repeat("A", maxSyslogMessageBytes+100)
	body := `{"entries":[{"name":"x","facility":"user","severity":"info","template":"` + big + `"}]}`
	_, err := parseSyslogCatalog([]byte(body), "<test>")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "MTU-safety") {
		t.Errorf("error %q: want mention of MTU-safety", err.Error())
	}
}

// TestSyslogCatalogPickDistribution verifies that Pick honours weights
// within a reasonable tolerance. 10000 draws over a 40/10 split should
// see the heavier entry ~8000 times (+/- a few %).
func TestSyslogCatalogPickDistribution(t *testing.T) {
	body := `{"entries":[
		{"name":"heavy","facility":"user","severity":"info","template":"h","weight":40},
		{"name":"light","facility":"user","severity":"info","template":"l","weight":10}
	]}`
	cat, err := parseSyslogCatalog([]byte(body), "<test>")
	if err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewSource(0xC0FFEE))
	counts := map[string]int{}
	const draws = 10000
	for i := 0; i < draws; i++ {
		e := cat.Pick(rnd)
		counts[e.Name]++
	}
	// Expected ratio: heavy:light = 40:10 = 4:1 → heavy ≈ 8000, light ≈ 2000.
	// Tolerance: ±5% of draws = ±500 in absolute count.
	const tol = 500
	if got := counts["heavy"]; got < 8000-tol || got > 8000+tol {
		t.Errorf("heavy count %d outside 8000±%d", got, tol)
	}
	if got := counts["light"]; got < 2000-tol || got > 2000+tol {
		t.Errorf("light count %d outside 2000±%d", got, tol)
	}
}

// TestSyslogCatalogResolveNoOverrides renders a template with IfIndex/IfName.
func TestSyslogCatalogResolveNoOverrides(t *testing.T) {
	cat, err := LoadEmbeddedSyslogCatalog()
	if err != nil {
		t.Fatal(err)
	}
	e := cat.ByName["interface-down"]
	if e == nil {
		t.Fatal("interface-down missing from embedded catalog")
	}
	ctx := SyslogTemplateCtx{
		DeviceIP: "10.42.0.1",
		IfIndex:  3,
		IfName:   "GigabitEthernet0/3",
		Now:      1700000000,
		Uptime:   123456,
	}
	msg, err := e.Resolve(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Message, "GigabitEthernet0/3") {
		t.Errorf("resolved message %q missing IfName", msg.Message)
	}
	if !strings.Contains(msg.Message, "ifIndex=3") {
		t.Errorf("resolved message %q missing IfIndex", msg.Message)
	}
	// Structured data should be populated with the same two fields.
	if len(msg.StructuredData) != 2 {
		t.Fatalf("structuredData pairs: got %d, want 2", len(msg.StructuredData))
	}
	gotSD := map[string]string{}
	for _, kv := range msg.StructuredData {
		gotSD[kv.Key] = kv.Value
	}
	if gotSD["ifIndex"] != "3" || gotSD["ifName"] != "GigabitEthernet0/3" {
		t.Errorf("structuredData resolved wrong: %v", gotSD)
	}
}

// TestSyslogCatalogResolveOverrides shows that templateOverrides from the
// HTTP endpoint pin template fields deterministically.
func TestSyslogCatalogResolveOverrides(t *testing.T) {
	cat, err := LoadEmbeddedSyslogCatalog()
	if err != nil {
		t.Fatal(err)
	}
	e := cat.ByName["interface-down"]
	ctx := SyslogTemplateCtx{IfIndex: 1, IfName: "lo"}
	msg, err := e.Resolve(ctx, map[string]string{
		"IfIndex": "7",
		"IfName":  "GigabitEthernet0/7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Message, "ifIndex=7") {
		t.Errorf("override did not apply: %q", msg.Message)
	}
	if !strings.Contains(msg.Message, "GigabitEthernet0/7") {
		t.Errorf("IfName override did not apply: %q", msg.Message)
	}
}

// TestSyslogCatalogResolveUnknownOverride rejects unknown override keys.
func TestSyslogCatalogResolveUnknownOverride(t *testing.T) {
	cat, err := LoadEmbeddedSyslogCatalog()
	if err != nil {
		t.Fatal(err)
	}
	e := cat.ByName["interface-down"]
	_, err = e.Resolve(SyslogTemplateCtx{}, map[string]string{"NotAField": "v"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "NotAField") {
		t.Errorf("error %q: want mention of NotAField", err.Error())
	}
}

// TestSyslogCatalogDuplicateName rejects two entries with the same name.
func TestSyslogCatalogDuplicateName(t *testing.T) {
	body := `{"entries":[
		{"name":"dup","facility":"user","severity":"info","template":"a"},
		{"name":"dup","facility":"user","severity":"info","template":"b"}
	]}`
	_, err := parseSyslogCatalog([]byte(body), "<test>")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate name") {
		t.Errorf("error %q: want 'duplicate name'", err.Error())
	}
}

// TestSyslogCatalogFacilityIntegerAccepted verifies both string name and
// integer syntax parse for facility (same for severity, implicitly covered
// by the out-of-range tests above).
func TestSyslogCatalogFacilityIntegerAccepted(t *testing.T) {
	body := `{"entries":[{"name":"x","facility":23,"severity":3,"template":"m"}]}`
	cat, err := parseSyslogCatalog([]byte(body), "<test>")
	if err != nil {
		t.Fatal(err)
	}
	if cat.Entries[0].Facility != 23 {
		t.Errorf("facility: got %d, want 23", cat.Entries[0].Facility)
	}
	if cat.Entries[0].Severity != 3 {
		t.Errorf("severity: got %d, want 3", cat.Entries[0].Severity)
	}
}
