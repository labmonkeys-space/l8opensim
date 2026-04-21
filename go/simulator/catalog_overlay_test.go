/*
 * © 2025 Labmonkeys Space
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 */

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadCatalog_ExtendsTrueMergesEntries loads a per-type catalog with
// `extends` defaulted (absent → true) and confirms its unique entry adds
// to the universal five.
func TestLoadCatalog_ExtendsTrueMergesEntries(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	overlayJSON := `{"traps":[{"name":"vendorUnique","snmpTrapOID":"1.3.6.1.4.1.9.1","varbinds":[]}]}`
	overlay := mustParseTrapCatalog(t, overlayJSON)
	if !overlay.Extends {
		t.Fatal("overlay.Extends: got false, want true (JSON omitted the field)")
	}
	merged := universal.MergeOverlay(overlay)
	if got := len(merged.Entries); got != len(universal.Entries)+1 {
		t.Errorf("merged entries: got %d, want %d (universal + 1)", got, len(universal.Entries)+1)
	}
	if _, ok := merged.ByName["vendorUnique"]; !ok {
		t.Error("merged catalog missing vendorUnique overlay entry")
	}
	if _, ok := merged.ByName["linkDown"]; !ok {
		t.Error("merged catalog missing universal linkDown entry")
	}
}

// TestLoadCatalog_ExtendsTrueOverridesSameName confirms that an overlay
// entry whose name matches a universal entry replaces it while leaving
// other universal entries intact.
func TestLoadCatalog_ExtendsTrueOverridesSameName(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	// Override linkDown with a distinctive snmpTrapOID so we can spot it.
	overlayJSON := `{"traps":[{"name":"linkDown","snmpTrapOID":"1.3.6.1.4.1.9.999","varbinds":[]}]}`
	overlay := mustParseTrapCatalog(t, overlayJSON)
	merged := universal.MergeOverlay(overlay)
	if got := len(merged.Entries); got != len(universal.Entries) {
		t.Errorf("merged entries count: got %d, want %d (same-name override preserves count)",
			got, len(universal.Entries))
	}
	got := merged.ByName["linkDown"]
	if got == nil {
		t.Fatal("linkDown missing after override")
	}
	if got.SnmpTrapOID != "1.3.6.1.4.1.9.999" {
		t.Errorf("linkDown OID: got %q, want override value %q", got.SnmpTrapOID, "1.3.6.1.4.1.9.999")
	}
}

// TestLoadCatalog_ExtendsFalseReplaces confirms that overlay.Extends=false
// signals pure replacement — callers that respect the flag skip the merge.
func TestLoadCatalog_ExtendsFalseReplaces(t *testing.T) {
	overlayJSON := `{"extends":false,"traps":[{"name":"onlyOne","snmpTrapOID":"1.2.3","varbinds":[]}]}`
	overlay := mustParseTrapCatalog(t, overlayJSON)
	if overlay.Extends {
		t.Fatal("overlay.Extends: got true, want false (explicit JSON)")
	}
	if len(overlay.Entries) != 1 {
		t.Errorf("overlay entries: got %d, want 1", len(overlay.Entries))
	}
}

// TestLoadCatalog_WeightsRecomputedPostMerge pins the merge-time weight
// math — total = universal + overlay (unique entries), same-name overrides
// use the overlay's weight.
func TestLoadCatalog_WeightsRecomputedPostMerge(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	universalTotal := universal.totalWeight
	overlayJSON := `{"traps":[
		{"name":"extraOne","snmpTrapOID":"1.2.3.1","weight":25,"varbinds":[]},
		{"name":"extraTwo","snmpTrapOID":"1.2.3.2","weight":15,"varbinds":[]}
	]}`
	overlay := mustParseTrapCatalog(t, overlayJSON)
	merged := universal.MergeOverlay(overlay)
	wantTotal := universalTotal + 25 + 15
	if merged.totalWeight != wantTotal {
		t.Errorf("merged totalWeight: got %d, want %d", merged.totalWeight, wantTotal)
	}
	// Sanity-check cumulativeW's terminal value equals totalWeight.
	if last := merged.cumulativeW[len(merged.cumulativeW)-1]; last != wantTotal {
		t.Errorf("merged cumulativeW last element: got %d, want %d", last, wantTotal)
	}
}

// TestLoadCatalog_InvalidPerTypeFailsStartupNamingPath confirms that a
// malformed per-type JSON file surfaces as a load error — the universal
// _fallback is never silently substituted.
func TestLoadCatalog_InvalidPerTypeFailsStartupNamingPath(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	tmp := t.TempDir()
	slugDir := filepath.Join(tmp, "badvendor")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(slugDir, "traps.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ScanPerTypeTrapCatalogs(universal, tmp)
	if err == nil {
		t.Fatal("expected scan error for malformed per-type JSON, got nil")
	}
	if !strings.Contains(err.Error(), "traps.json") {
		t.Errorf("error should name the offending file path, got %v", err)
	}
}

// TestScanPerTypeTrapCatalogs_MissingDirIsNotAnError matches the existing
// SNMP-resource loader's tolerance of a missing resource tree.
func TestScanPerTypeTrapCatalogs_MissingDirIsNotAnError(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	result, err := ScanPerTypeTrapCatalogs(universal, filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result map, got %d entries", len(result))
	}
}

// TestScanPerTypeTrapCatalogs_SkipsReservedCommonDir confirms the loader
// does not treat `resources/_common/` as a per-type overlay — that dir is
// the embedded-universal's on-disk home.
func TestScanPerTypeTrapCatalogs_SkipsReservedCommonDir(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	tmp := t.TempDir()
	commonDir := filepath.Join(tmp, "_common")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a deliberately-broken file there; if the loader were going to
	// pick it up, we'd see a parse error. Silence = the dir was skipped.
	if err := os.WriteFile(filepath.Join(commonDir, "traps.json"), []byte("BROKEN"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ScanPerTypeTrapCatalogs(universal, tmp)
	if err != nil {
		t.Errorf("_common dir should have been skipped, got error: %v", err)
	}
	if _, ok := result["_common"]; ok {
		t.Error("result map should not contain _common key")
	}
}

// TestScanPerTypeTrapCatalogs_ExtendsFalsePropagates confirms that a
// per-type file with extends=false ends up as a pure replacement in the
// returned map (no universal entries carry through).
func TestScanPerTypeTrapCatalogs_ExtendsFalsePropagates(t *testing.T) {
	universal := mustLoadEmbeddedCatalog(t)
	tmp := t.TempDir()
	slugDir := filepath.Join(tmp, "purevendor")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"extends":false,"traps":[{"name":"onlyVendor","snmpTrapOID":"1.2.3","varbinds":[]}]}`
	if err := os.WriteFile(filepath.Join(slugDir, "traps.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ScanPerTypeTrapCatalogs(universal, tmp)
	if err != nil {
		t.Fatal(err)
	}
	got := result["purevendor"]
	if got == nil {
		t.Fatal("purevendor missing from scan result")
	}
	if len(got.Entries) != 1 {
		t.Errorf("purevendor entries: got %d, want 1 (pure replacement, no universal)", len(got.Entries))
	}
	if _, ok := got.ByName["linkDown"]; ok {
		t.Error("purevendor should not contain linkDown (extends=false means no universal)")
	}
}

// mustParseTrapCatalog loads raw JSON as a trap catalog, failing the test
// on any parse error. Used by overlay tests that embed fixtures inline.
func mustParseTrapCatalog(t *testing.T, body string) *Catalog {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "c.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalogFromFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cat
}

// mustLoadEmbeddedCatalog is a test helper that fails on the unlikely
// embedded-catalog read error rather than making every caller handle it.
func mustLoadEmbeddedCatalog(t *testing.T) *Catalog {
	t.Helper()
	cat, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatalf("LoadEmbeddedCatalog: %v", err)
	}
	return cat
}
