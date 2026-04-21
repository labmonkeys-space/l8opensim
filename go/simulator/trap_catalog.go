/*
 * © 2025 Labmonkeys Space
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 * You may obtain a copy of the License at:
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// SNMP trap catalog loader and selector.
//
// Resolves the two catalog open questions from design.md:
//   - OQ #1: weighted-random selection with per-entry `weight` (default 1).
//     Universal catalog weights: linkDown=40, linkUp=40, authenticationFailure=10,
//     coldStart=5, warmStart=5.
//   - OQ #3: embedded catalog path is resources/_common/traps.json.

package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"text/template"
)

//go:embed resources/_common/traps.json
var embeddedCatalogFS embed.FS

const embeddedCatalogPath = "resources/_common/traps.json"

// Reserved OIDs that the encoder prepends automatically to every trap.
// Catalog authors MUST NOT include them in body varbinds.
// - `sysUpTime.0` and `snmpTrapOID.0` are prepended unconditionally (design.md §D10).
// - `snmpTrapEnterprise.0` is prepended when the catalog entry sets the
//   optional `snmpTrapEnterprise` field (follow-up issue #100).
const (
	oidSysUpTime0          = "1.3.6.1.2.1.1.3.0"
	oidSnmpTrapOID0        = "1.3.6.1.6.3.1.1.4.1.0"
	oidSnmpTrapEnterprise0 = "1.3.6.1.6.3.1.1.4.3.0"
)

// allowedTemplateFields enumerates the four template fields the catalog
// grammar supports. Any other {{.Name}} reference in OID or value strings
// is rejected at load time.
var allowedTemplateFields = map[string]struct{}{
	"IfIndex":  {},
	"Uptime":   {},
	"Now":      {},
	"DeviceIP": {},
}

// TrapVarbindType identifies the ASN.1 application type used when encoding
// a varbind's value on the wire. Parsed from the catalog JSON "type" field.
type TrapVarbindType string

const (
	TrapVTInteger     TrapVarbindType = "integer"
	TrapVTOctetString TrapVarbindType = "octet-string"
	TrapVTOID         TrapVarbindType = "oid"
	TrapVTCounter32   TrapVarbindType = "counter32"
	TrapVTGauge32     TrapVarbindType = "gauge32"
	TrapVTTimeTicks   TrapVarbindType = "timeticks"
	TrapVTCounter64   TrapVarbindType = "counter64"
	TrapVTIPAddress   TrapVarbindType = "ipaddress"
)

// trapVarbindJSON is the on-disk shape of a single catalog varbind entry.
// Templates in `oid` and `value` are resolved per-fire.
type trapVarbindJSON struct {
	OID   string          `json:"oid"`
	Type  TrapVarbindType `json:"type"`
	Value string          `json:"value"`
}

// catalogEntryJSON is the on-disk shape of one trap catalog entry.
type catalogEntryJSON struct {
	Name               string            `json:"name"`
	SnmpTrapOID        string            `json:"snmpTrapOID"`
	SnmpTrapEnterprise string            `json:"snmpTrapEnterprise,omitempty"`
	Weight             int               `json:"weight"`
	Varbinds           []trapVarbindJSON `json:"varbinds"`
}

// trapCatalogJSON is the on-disk shape of the whole catalog file.
// The "comment" field (if present) is ignored so authors can annotate files.
type trapCatalogJSON struct {
	Comment string             `json:"comment,omitempty"`
	Traps   []catalogEntryJSON `json:"traps"`
}

// VarbindTemplate is one parsed, template-compiled varbind. Templates for OID
// and Value are pre-compiled at catalog load so Resolve runs in microseconds
// even under 30k-device / 1000 tps load.
type VarbindTemplate struct {
	Type     TrapVarbindType
	OIDTmpl  *template.Template
	ValTmpl  *template.Template
	rawOID   string // for error messages
	rawValue string
}

// CatalogEntry is one parsed trap in the catalog. Weight defaults to 1 when
// omitted from JSON; Pick is weight-biased random selection.
//
// SnmpTrapEnterprise is the OPTIONAL value for the `snmpTrapEnterprise.0`
// varbind (OID 1.3.6.1.6.3.1.1.4.3.0) from SNMPv2-MIB §10. When non-empty,
// the encoder auto-prepends this varbind after the two mandatory
// (`sysUpTime.0`, `snmpTrapOID.0`) and before the body varbinds. Empty
// means the varbind is not emitted — backward-compatible with catalogs
// authored before this field existed.
type CatalogEntry struct {
	Name               string
	SnmpTrapOID        string
	SnmpTrapEnterprise string
	Weight             int
	Varbinds           []VarbindTemplate
}

// Catalog is the whole parsed trap catalog plus cached weight metadata for Pick.
// Immutable after load; safe for concurrent read from every device.
type Catalog struct {
	Entries       []*CatalogEntry
	ByName        map[string]*CatalogEntry
	cumulativeW   []int // cumulativeW[i] = sum(Weight[0..i]); used by Pick
	totalWeight   int
}

// TemplateCtx is the data handed to text/template when Resolve evaluates
// per-fire. Exactly matches the four fields in allowedTemplateFields.
type TemplateCtx struct {
	IfIndex  int
	Uptime   uint32 // 1/100s ticks since device start
	Now      int64  // Unix epoch seconds
	DeviceIP string // dotted-quad
}

// Varbind is one resolved (templates evaluated) varbind ready for the encoder.
type Varbind struct {
	OID   string
	Type  TrapVarbindType
	Value string
}

// LoadEmbeddedCatalog parses the universal catalog compiled into the binary
// via //go:embed. The feature works out-of-box — no -trap-catalog flag or
// filesystem access is needed for the default catalog.
func LoadEmbeddedCatalog() (*Catalog, error) {
	data, err := embeddedCatalogFS.ReadFile(embeddedCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("trap catalog: embedded read failed: %w", err)
	}
	return parseCatalog(data, "<embedded "+embeddedCatalogPath+">")
}

// LoadCatalogFromFile parses a user-supplied catalog file. Replaces the
// embedded catalog entirely — there is no merge (design.md §D3).
func LoadCatalogFromFile(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("trap catalog: reading %q: %w", path, err)
	}
	return parseCatalog(data, path)
}

// parseCatalog is the shared body of the two Load* helpers. Source is used
// in error messages only.
func parseCatalog(data []byte, source string) (*Catalog, error) {
	var doc trapCatalogJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("trap catalog: parsing %s: %w", source, err)
	}
	if len(doc.Traps) == 0 {
		return nil, fmt.Errorf("trap catalog: %s has no entries", source)
	}

	cat := &Catalog{
		Entries: make([]*CatalogEntry, 0, len(doc.Traps)),
		ByName:  make(map[string]*CatalogEntry, len(doc.Traps)),
	}
	for i, raw := range doc.Traps {
		entry, err := compileEntry(raw, source, i)
		if err != nil {
			return nil, err
		}
		if _, dup := cat.ByName[entry.Name]; dup {
			return nil, fmt.Errorf("trap catalog: %s entry %d: duplicate name %q", source, i, entry.Name)
		}
		cat.Entries = append(cat.Entries, entry)
		cat.ByName[entry.Name] = entry
	}

	// Precompute cumulative weights for Pick's O(log N) binary search later.
	// Linear scan is fine at catalog load since catalogs are small (≤ a few dozen).
	running := 0
	cat.cumulativeW = make([]int, len(cat.Entries))
	for i, e := range cat.Entries {
		running += e.Weight
		cat.cumulativeW[i] = running
	}
	cat.totalWeight = running
	if cat.totalWeight <= 0 {
		return nil, fmt.Errorf("trap catalog: %s total weight must be > 0", source)
	}
	return cat, nil
}

// compileEntry validates and compiles one catalog entry. Rejects reserved
// varbind OIDs (design.md §D10) and templates that reference fields outside
// the allowed four (spec: "Unknown template field rejected").
func compileEntry(raw catalogEntryJSON, source string, idx int) (*CatalogEntry, error) {
	if raw.Name == "" {
		return nil, fmt.Errorf("trap catalog: %s entry %d: name is required", source, idx)
	}
	if raw.SnmpTrapOID == "" {
		return nil, fmt.Errorf("trap catalog: %s entry %q: snmpTrapOID is required", source, raw.Name)
	}
	weight := raw.Weight
	if weight == 0 {
		weight = 1 // OQ#1: default weight = 1
	}
	if weight < 0 {
		return nil, fmt.Errorf("trap catalog: %s entry %q: weight must be non-negative", source, raw.Name)
	}

	entry := &CatalogEntry{
		Name:               raw.Name,
		SnmpTrapOID:        strings.TrimPrefix(raw.SnmpTrapOID, "."),
		SnmpTrapEnterprise: strings.TrimPrefix(raw.SnmpTrapEnterprise, "."),
		Weight:             weight,
		Varbinds:           make([]VarbindTemplate, 0, len(raw.Varbinds)),
	}
	// The optional snmpTrapEnterprise.0 value must not collide with OIDs
	// the encoder auto-prepends. snmpTrapOID.0 / sysUpTime.0 as an
	// enterprise value would be nonsensical; snmpTrapEnterprise.0 itself
	// is the OID of the varbind, not its value.
	if entry.SnmpTrapEnterprise != "" {
		switch entry.SnmpTrapEnterprise {
		case oidSysUpTime0, oidSnmpTrapOID0, oidSnmpTrapEnterprise0:
			return nil, fmt.Errorf("trap catalog: %s entry %q: snmpTrapEnterprise value %s "+
				"is a reserved OID — the field should hold an enterprise OID reflecting "+
				"the notification type, typically the parent of snmpTrapOID",
				source, raw.Name, raw.SnmpTrapEnterprise)
		}
	}
	for j, vb := range raw.Varbinds {
		if err := validateVarbindOID(vb.OID, raw.Name, j); err != nil {
			return nil, err
		}
		if vb.Type == "" {
			return nil, fmt.Errorf("trap catalog: %s entry %q varbind %d: type is required", source, raw.Name, j)
		}
		switch vb.Type {
		case TrapVTInteger, TrapVTOctetString, TrapVTOID,
			TrapVTCounter32, TrapVTGauge32, TrapVTTimeTicks,
			TrapVTCounter64, TrapVTIPAddress:
			// ok
		default:
			return nil, fmt.Errorf("trap catalog: %s entry %q varbind %d: unknown type %q",
				source, raw.Name, j, vb.Type)
		}
		if err := validateTemplateFields(vb.OID, raw.Name, j, "oid"); err != nil {
			return nil, err
		}
		if err := validateTemplateFields(vb.Value, raw.Name, j, "value"); err != nil {
			return nil, err
		}

		oidTmpl, err := template.New(raw.Name + ".vb" + fmt.Sprint(j) + ".oid").Parse(vb.OID)
		if err != nil {
			return nil, fmt.Errorf("trap catalog: %s entry %q varbind %d: oid template parse: %w",
				source, raw.Name, j, err)
		}
		valTmpl, err := template.New(raw.Name + ".vb" + fmt.Sprint(j) + ".value").Parse(vb.Value)
		if err != nil {
			return nil, fmt.Errorf("trap catalog: %s entry %q varbind %d: value template parse: %w",
				source, raw.Name, j, err)
		}

		entry.Varbinds = append(entry.Varbinds, VarbindTemplate{
			Type:     vb.Type,
			OIDTmpl:  oidTmpl,
			ValTmpl:  valTmpl,
			rawOID:   vb.OID,
			rawValue: vb.Value,
		})
	}
	return entry, nil
}

// validateVarbindOID rejects the reserved OIDs that the encoder prepends
// automatically. Accepts templates (anything containing "{{") as a pass — the
// reserved OIDs are literal strings, so a template'd OID cannot collide.
//
// snmpTrapEnterprise.0 is rejected in body varbinds because it has its own
// top-level catalog field (`snmpTrapEnterprise`); including it in body
// varbinds would produce a duplicate on the wire.
func validateVarbindOID(raw, entryName string, idx int) error {
	if strings.Contains(raw, "{{") {
		return nil
	}
	norm := strings.TrimPrefix(raw, ".")
	switch norm {
	case oidSysUpTime0, oidSnmpTrapOID0:
		return fmt.Errorf("trap catalog: entry %q varbind %d: OID %s is reserved "+
			"(sysUpTime.0 and snmpTrapOID.0 are prepended automatically by the encoder)",
			entryName, idx, raw)
	case oidSnmpTrapEnterprise0:
		return fmt.Errorf("trap catalog: entry %q varbind %d: OID %s is reserved — "+
			"use the entry-level `snmpTrapEnterprise` field instead of a body varbind",
			entryName, idx, raw)
	}
	return nil
}

// validateTemplateFields finds every {{.Name}} reference in s and rejects any
// Name that isn't in allowedTemplateFields. The check runs at catalog load,
// BEFORE text/template parses — we want a clearer error message than the
// undefined-variable error text/template would produce at evaluation time.
func validateTemplateFields(s, entryName string, vbIdx int, which string) error {
	// Scan for `{{.Ident}}` tokens. We intentionally accept only the simple
	// field-access form; pipelines, functions, and ranges are out of grammar.
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			return nil
		}
		close := strings.Index(rest[open:], "}}")
		if close < 0 {
			return fmt.Errorf("trap catalog: entry %q varbind %d %s: unterminated %q",
				entryName, vbIdx, which, "{{")
		}
		expr := strings.TrimSpace(rest[open+2 : open+close])
		if !strings.HasPrefix(expr, ".") {
			return fmt.Errorf("trap catalog: entry %q varbind %d %s: only simple field "+
				"access is allowed (e.g. {{.IfIndex}}); got %q",
				entryName, vbIdx, which, expr)
		}
		field := strings.TrimPrefix(expr, ".")
		if strings.ContainsAny(field, " \t\n|(){}") {
			return fmt.Errorf("trap catalog: entry %q varbind %d %s: only simple field "+
				"access is allowed (e.g. {{.IfIndex}}); got %q",
				entryName, vbIdx, which, expr)
		}
		if _, ok := allowedTemplateFields[field]; !ok {
			return fmt.Errorf("trap catalog: entry %q varbind %d %s: unknown template field "+
				"%q (allowed: IfIndex, Uptime, Now, DeviceIP)",
				entryName, vbIdx, which, field)
		}
		rest = rest[open+close+2:]
	}
}

// Pick selects a catalog entry via weighted-random draw. rnd must be non-nil.
func (c *Catalog) Pick(rnd *rand.Rand) *CatalogEntry {
	if c == nil || len(c.Entries) == 0 {
		return nil
	}
	if c.totalWeight <= 0 {
		return nil
	}
	// Linear scan: catalog sizes are small (≤ tens), so log-N binary search
	// isn't worth the cache miss. If catalogs grow, revisit.
	r := rnd.Intn(c.totalWeight)
	for i, cum := range c.cumulativeW {
		if r < cum {
			return c.Entries[i]
		}
	}
	return c.Entries[len(c.Entries)-1]
}

// Resolve evaluates the entry's templates against ctx and overrides, producing
// Varbinds ready for the PDU encoder. overrides, when non-nil, replace the
// corresponding fields in ctx (e.g. "IfIndex": "7" pins that field for the
// POST /api/v1/devices/{ip}/trap use-case).
func (e *CatalogEntry) Resolve(ctx TemplateCtx, overrides map[string]string) ([]Varbind, error) {
	if len(overrides) > 0 {
		if v, ok := overrides["IfIndex"]; ok {
			n, err := parseIntField(v, "IfIndex")
			if err != nil {
				return nil, err
			}
			ctx.IfIndex = n
		}
		if v, ok := overrides["Uptime"]; ok {
			n, err := parseIntField(v, "Uptime")
			if err != nil {
				return nil, err
			}
			ctx.Uptime = uint32(n)
		}
		if v, ok := overrides["Now"]; ok {
			n, err := parseIntField(v, "Now")
			if err != nil {
				return nil, err
			}
			ctx.Now = int64(n)
		}
		if v, ok := overrides["DeviceIP"]; ok {
			ctx.DeviceIP = v
		}
		for k := range overrides {
			if _, ok := allowedTemplateFields[k]; !ok {
				return nil, fmt.Errorf("trap varbind override: unknown field %q", k)
			}
		}
	}

	out := make([]Varbind, 0, len(e.Varbinds))
	var buf bytes.Buffer
	for i, vt := range e.Varbinds {
		buf.Reset()
		if err := vt.OIDTmpl.Execute(&buf, ctx); err != nil {
			return nil, fmt.Errorf("trap %q varbind %d oid resolve: %w", e.Name, i, err)
		}
		oid := buf.String()
		buf.Reset()
		if err := vt.ValTmpl.Execute(&buf, ctx); err != nil {
			return nil, fmt.Errorf("trap %q varbind %d value resolve: %w", e.Name, i, err)
		}
		out = append(out, Varbind{
			OID:   oid,
			Type:  vt.Type,
			Value: buf.String(),
		})
	}
	return out, nil
}

func parseIntField(s, name string) (int, error) {
	// Tolerant parse: the HTTP body ships overrides as strings, but the
	// template fields are integers.
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("trap varbind override %s: expected integer, got %q", name, s)
	}
	return n, nil
}
