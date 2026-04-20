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

// Syslog catalog loader and selector.
//
// Mirrors the shape of trap_catalog.go — same embedded FS pattern, same
// weighted-random Pick, same strict text/template field validation. Key
// differences from the trap catalog:
//   - entries carry RFC 5424 / 3164 metadata (facility, severity, appName,
//     msgId, structuredData) instead of SNMP varbinds
//   - the "varbind template" vocabulary is six fields instead of four
//     (adds SysName, IfName)
//   - an MTU-safety dry-render at load time rejects catalog entries whose
//     rendered output could exceed 1400 bytes (design.md §D12)

package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"text/template"
)

//go:embed resources/_common/syslog.json
var embeddedSyslogCatalogFS embed.FS

const embeddedSyslogCatalogPath = "resources/_common/syslog.json"

// maxSyslogMessageBytes is the dry-render ceiling enforced at catalog load
// time (design.md §D12). 1400 leaves headroom below the typical 1500-byte
// Ethernet MTU for IP + UDP headers and any small collector-side framing.
const maxSyslogMessageBytes = 1400

// allowedSyslogTemplateFields enumerates the six template fields the catalog
// grammar supports (design.md §D4). Any other {{.Name}} reference in
// `template` or `hostname` strings is rejected at load time.
var allowedSyslogTemplateFields = map[string]struct{}{
	"DeviceIP": {},
	"SysName":  {},
	"IfIndex":  {},
	"IfName":   {},
	"Now":      {},
	"Uptime":   {},
}

// SyslogFacility is an RFC 5424 facility value (0..23). We store the numeric
// form post-parse; the catalog JSON accepts either canonical names or ints.
type SyslogFacility uint8

// SyslogSeverity is an RFC 5424 severity value (0..7).
type SyslogSeverity uint8

// Canonical facility names per RFC 5424 Table 1. The map is used to translate
// the catalog's string form into the numeric form stored on parsed entries.
var syslogFacilityNames = map[string]SyslogFacility{
	"kern":     0,
	"user":     1,
	"mail":     2,
	"daemon":   3,
	"auth":     4,
	"syslog":   5,
	"lpr":      6,
	"news":     7,
	"uucp":     8,
	"cron":     9,
	"authpriv": 10,
	"ftp":      11,
	"ntp":      12,
	"audit":    13,
	"alert":    14,
	"clock":    15,
	"local0":   16,
	"local1":   17,
	"local2":   18,
	"local3":   19,
	"local4":   20,
	"local5":   21,
	"local6":   22,
	"local7":   23,
}

// Canonical severity names per RFC 5424 Table 2. Keys are lowercase so the
// catalog accepts "error" and "err" interchangeably.
var syslogSeverityNames = map[string]SyslogSeverity{
	"emerg":   0,
	"alert":   1,
	"crit":    2,
	"err":     3,
	"error":   3,
	"warning": 4,
	"warn":    4,
	"notice":  5,
	"info":    6,
	"debug":   7,
}

// syslogFacilityJSON is the JSON-friendly form of a facility value: accepts
// either an integer (0..23) or a canonical string name. We implement
// UnmarshalJSON rather than forcing the catalog author to pick one form.
type syslogFacilityJSON struct {
	value SyslogFacility
	set   bool
}

func (f *syslogFacilityJSON) UnmarshalJSON(data []byte) error {
	var asInt int
	if err := json.Unmarshal(data, &asInt); err == nil {
		if asInt < 0 || asInt > 23 {
			return fmt.Errorf("facility integer %d out of range (0..23)", asInt)
		}
		f.value = SyslogFacility(asInt)
		f.set = true
		return nil
	}
	var asStr string
	if err := json.Unmarshal(data, &asStr); err != nil {
		return fmt.Errorf("facility must be a string name or integer (got %s)", string(data))
	}
	v, ok := syslogFacilityNames[strings.ToLower(asStr)]
	if !ok {
		return fmt.Errorf("unknown facility name %q (valid: kern, user, mail, daemon, auth, "+
			"syslog, lpr, news, uucp, cron, authpriv, ftp, ntp, audit, alert, clock, "+
			"local0..local7)", asStr)
	}
	f.value = v
	f.set = true
	return nil
}

// syslogSeverityJSON mirrors syslogFacilityJSON for severities (0..7).
type syslogSeverityJSON struct {
	value SyslogSeverity
	set   bool
}

func (s *syslogSeverityJSON) UnmarshalJSON(data []byte) error {
	var asInt int
	if err := json.Unmarshal(data, &asInt); err == nil {
		if asInt < 0 || asInt > 7 {
			return fmt.Errorf("severity integer %d out of range (0..7)", asInt)
		}
		s.value = SyslogSeverity(asInt)
		s.set = true
		return nil
	}
	var asStr string
	if err := json.Unmarshal(data, &asStr); err != nil {
		return fmt.Errorf("severity must be a string name or integer (got %s)", string(data))
	}
	v, ok := syslogSeverityNames[strings.ToLower(asStr)]
	if !ok {
		return fmt.Errorf("unknown severity name %q (valid: emerg, alert, crit, err/error, "+
			"warning/warn, notice, info, debug)", asStr)
	}
	s.value = v
	s.set = true
	return nil
}

// syslogCatalogEntryJSON is the on-disk shape of one catalog entry.
type syslogCatalogEntryJSON struct {
	Name           string              `json:"name"`
	Weight         int                 `json:"weight"`
	Facility       syslogFacilityJSON  `json:"facility"`
	Severity       syslogSeverityJSON  `json:"severity"`
	AppName        string              `json:"appName"`
	MsgID          string              `json:"msgId"`
	StructuredData map[string]string   `json:"structuredData"`
	Hostname       string              `json:"hostname"`
	Template       string              `json:"template"`
}

// syslogCatalogJSON is the on-disk shape of the whole catalog file. The
// "comment" field is ignored so authors can annotate files.
type syslogCatalogJSON struct {
	Comment string                   `json:"comment,omitempty"`
	Entries []syslogCatalogEntryJSON `json:"entries"`
}

// SyslogCatalogEntry is one parsed catalog entry. `TemplateTmpl` and
// `HostnameTmpl` are nil when the corresponding source string was empty;
// callers must check before invoking Execute. `StructuredData` is flattened
// to a stable sorted slice of [key, value] pairs so RFC 5424 output is
// deterministic across renders.
type SyslogCatalogEntry struct {
	Name               string
	Weight             int
	Facility           SyslogFacility
	Severity           SyslogSeverity
	AppName            string
	MsgID              string
	StructuredDataKeys []string          // sorted; empty when no SD
	StructuredData     map[string]string // same data, by key
	Hostname           string            // raw source — empty means "use default derivation"
	HostnameTmpl       *template.Template
	Template           string // raw source — may be empty
	TemplateTmpl       *template.Template
}

// SyslogCatalog is the whole parsed catalog plus cached weight metadata for
// Pick. Immutable after load; safe for concurrent read.
type SyslogCatalog struct {
	Entries     []*SyslogCatalogEntry
	ByName      map[string]*SyslogCatalogEntry
	cumulativeW []int
	totalWeight int
}

// SyslogTemplateCtx is the data passed to text/template at fire time (design
// §D4). Must match `allowedSyslogTemplateFields` exactly.
type SyslogTemplateCtx struct {
	DeviceIP string
	SysName  string
	IfIndex  int
	IfName   string
	Now      int64  // Unix epoch seconds
	Uptime   uint32 // 1/100s ticks since device start
}

// SyslogResolved is a catalog entry rendered against a concrete context. It
// is the direct input to the wire encoders in syslog_wire.go.
type SyslogResolved struct {
	Facility       SyslogFacility
	Severity       SyslogSeverity
	AppName        string
	MsgID          string
	Hostname       string // empty means "caller derives from SysName/DeviceIP"
	StructuredData []SyslogSDPair
	Message        string
}

// SyslogSDPair is one structured-data key/value after template resolution.
type SyslogSDPair struct {
	Key   string
	Value string
}

// LoadEmbeddedSyslogCatalog parses the universal catalog compiled into the
// binary via //go:embed. The feature works out-of-box — no -syslog-catalog
// flag or filesystem access is needed for the default catalog.
func LoadEmbeddedSyslogCatalog() (*SyslogCatalog, error) {
	data, err := embeddedSyslogCatalogFS.ReadFile(embeddedSyslogCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("syslog catalog: embedded read failed: %w", err)
	}
	return parseSyslogCatalog(data, "<embedded "+embeddedSyslogCatalogPath+">")
}

// LoadSyslogCatalogFromFile parses a user-supplied catalog file. Replaces the
// embedded catalog entirely — there is no merge (design.md §D11).
func LoadSyslogCatalogFromFile(path string) (*SyslogCatalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("syslog catalog: reading %q: %w", path, err)
	}
	return parseSyslogCatalog(data, path)
}

func parseSyslogCatalog(data []byte, source string) (*SyslogCatalog, error) {
	var doc syslogCatalogJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("syslog catalog: parsing %s: %w", source, err)
	}
	if len(doc.Entries) == 0 {
		return nil, fmt.Errorf("syslog catalog: %s has no entries", source)
	}

	cat := &SyslogCatalog{
		Entries: make([]*SyslogCatalogEntry, 0, len(doc.Entries)),
		ByName:  make(map[string]*SyslogCatalogEntry, len(doc.Entries)),
	}
	for i, raw := range doc.Entries {
		entry, err := compileSyslogEntry(raw, source, i)
		if err != nil {
			return nil, err
		}
		if _, dup := cat.ByName[entry.Name]; dup {
			return nil, fmt.Errorf("syslog catalog: %s entry %d: duplicate name %q", source, i, entry.Name)
		}
		cat.Entries = append(cat.Entries, entry)
		cat.ByName[entry.Name] = entry
	}

	running := 0
	cat.cumulativeW = make([]int, len(cat.Entries))
	for i, e := range cat.Entries {
		running += e.Weight
		cat.cumulativeW[i] = running
	}
	cat.totalWeight = running
	if cat.totalWeight <= 0 {
		return nil, fmt.Errorf("syslog catalog: %s total weight must be > 0", source)
	}
	return cat, nil
}

func compileSyslogEntry(raw syslogCatalogEntryJSON, source string, idx int) (*SyslogCatalogEntry, error) {
	if raw.Name == "" {
		return nil, fmt.Errorf("syslog catalog: %s entry %d: name is required", source, idx)
	}
	if !raw.Facility.set {
		return nil, fmt.Errorf("syslog catalog: %s entry %q: facility is required", source, raw.Name)
	}
	if !raw.Severity.set {
		return nil, fmt.Errorf("syslog catalog: %s entry %q: severity is required", source, raw.Name)
	}
	weight := raw.Weight
	if weight == 0 {
		weight = 1
	}
	if weight < 0 {
		return nil, fmt.Errorf("syslog catalog: %s entry %q: weight must be non-negative", source, raw.Name)
	}

	entry := &SyslogCatalogEntry{
		Name:     raw.Name,
		Weight:   weight,
		Facility: raw.Facility.value,
		Severity: raw.Severity.value,
		AppName:  raw.AppName,
		MsgID:    raw.MsgID,
		Hostname: raw.Hostname,
		Template: raw.Template,
	}
	if len(raw.StructuredData) > 0 {
		entry.StructuredData = make(map[string]string, len(raw.StructuredData))
		entry.StructuredDataKeys = make([]string, 0, len(raw.StructuredData))
		for k, v := range raw.StructuredData {
			entry.StructuredData[k] = v
			entry.StructuredDataKeys = append(entry.StructuredDataKeys, k)
		}
		sort.Strings(entry.StructuredDataKeys)
	}

	// Validate template fields in every templatable string before parsing
	// the templates themselves. This gives a clearer error than
	// text/template's "undefined variable" message at evaluation time.
	for _, pair := range []struct {
		value string
		which string
	}{
		{raw.Template, "template"},
		{raw.Hostname, "hostname"},
	} {
		if pair.value == "" {
			continue
		}
		if err := validateSyslogTemplateFields(pair.value, raw.Name, pair.which); err != nil {
			return nil, err
		}
	}
	for k, v := range raw.StructuredData {
		if err := validateSyslogTemplateFields(v, raw.Name, "structuredData."+k); err != nil {
			return nil, err
		}
	}

	if raw.Template != "" {
		tmpl, err := template.New(raw.Name + ".template").Parse(raw.Template)
		if err != nil {
			return nil, fmt.Errorf("syslog catalog: %s entry %q template parse: %w", source, raw.Name, err)
		}
		entry.TemplateTmpl = tmpl
	}
	if raw.Hostname != "" {
		tmpl, err := template.New(raw.Name + ".hostname").Parse(raw.Hostname)
		if err != nil {
			return nil, fmt.Errorf("syslog catalog: %s entry %q hostname parse: %w", source, raw.Name, err)
		}
		entry.HostnameTmpl = tmpl
	}

	// MTU-safety dry-render (design.md §D12). We render with worst-case values
	// for every template field and reject the entry at load time if the total
	// 5424 message bytes exceed maxSyslogMessageBytes. 5424 is always at least
	// as large as 3164 for the same inputs (structured data + BOM overhead),
	// so checking 5424 is sufficient.
	if err := validateSyslogEntrySize(entry, source); err != nil {
		return nil, err
	}
	return entry, nil
}

// validateSyslogTemplateFields scans s for `{{.Ident}}` tokens and rejects
// any Ident outside the approved six fields. Pipelines, functions, and
// ranges are out of grammar.
func validateSyslogTemplateFields(s, entryName, which string) error {
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			return nil
		}
		closeIdx := strings.Index(rest[open:], "}}")
		if closeIdx < 0 {
			return fmt.Errorf("syslog catalog: entry %q %s: unterminated %q", entryName, which, "{{")
		}
		expr := strings.TrimSpace(rest[open+2 : open+closeIdx])
		if !strings.HasPrefix(expr, ".") {
			return fmt.Errorf("syslog catalog: entry %q %s: only simple field access is allowed "+
				"(e.g. {{.IfIndex}}); got %q", entryName, which, expr)
		}
		field := strings.TrimPrefix(expr, ".")
		if strings.ContainsAny(field, " \t\n|(){}") {
			return fmt.Errorf("syslog catalog: entry %q %s: only simple field access is allowed "+
				"(e.g. {{.IfIndex}}); got %q", entryName, which, expr)
		}
		if _, ok := allowedSyslogTemplateFields[field]; !ok {
			return fmt.Errorf("syslog catalog: entry %q %s: unknown template field %q "+
				"(allowed: DeviceIP, SysName, IfIndex, IfName, Now, Uptime)",
				entryName, which, field)
		}
		rest = rest[open+closeIdx+2:]
	}
}

// validateSyslogEntrySize renders the entry against a worst-case context and
// rejects it if the 5424 byte length exceeds maxSyslogMessageBytes. The
// worst-case context uses the longest plausible values for every template
// field (design.md §D12) so the check is an upper bound across all devices.
func validateSyslogEntrySize(entry *SyslogCatalogEntry, source string) error {
	worst := SyslogTemplateCtx{
		DeviceIP: "255.255.255.255",
		SysName:  strings.Repeat("H", 64),
		IfIndex:  65535,
		IfName:   strings.Repeat("X", 32),
		Now:      9999999999,
		Uptime:   0xFFFFFFFF,
	}
	resolved, err := entry.resolveAgainst(worst, nil)
	if err != nil {
		return fmt.Errorf("syslog catalog: %s entry %q dry-render: %w", source, entry.Name, err)
	}
	// The 5424 encoder is the heavier of the two formats; use it to size.
	size := estimateRFC5424Size(resolved, worst.SysName)
	if size > maxSyslogMessageBytes {
		return fmt.Errorf("syslog catalog: %s entry %q: dry-rendered size %d exceeds MTU-safety "+
			"ceiling of %d bytes — shorten the template or structured-data fields",
			source, entry.Name, size, maxSyslogMessageBytes)
	}
	return nil
}

// estimateRFC5424Size computes a reasonable upper bound on the emitted 5424
// datagram size for `r`. It doesn't do the full format pass (that's in
// syslog_wire.go); it just sums the field lengths plus conservative per-field
// overhead. Used only for catalog-load MTU validation.
func estimateRFC5424Size(r SyslogResolved, fallbackHost string) int {
	host := r.Hostname
	if host == "" {
		host = fallbackHost
	}
	// `<PRI>` up to 5 bytes, version+timestamp+BOM ≈ 34 bytes, 5 separator
	// spaces, 4 NILVALUE dashes on a bare message. Round to 64 for safety.
	overhead := 64
	sdLen := 0
	for _, kv := range r.StructuredData {
		sdLen += len(kv.Key) + len(kv.Value) + 4 // `key="..."`
	}
	if sdLen > 0 {
		sdLen += 32 // `[meta@32473 ...]` wrapping
	}
	return overhead + len(host) + len(r.AppName) + len(r.MsgID) + sdLen + len(r.Message)
}

// Pick selects a catalog entry via weighted-random draw. rnd must be non-nil.
func (c *SyslogCatalog) Pick(rnd *rand.Rand) *SyslogCatalogEntry {
	if c == nil || len(c.Entries) == 0 || c.totalWeight <= 0 {
		return nil
	}
	r := rnd.Intn(c.totalWeight)
	for i, cum := range c.cumulativeW {
		if r < cum {
			return c.Entries[i]
		}
	}
	return c.Entries[len(c.Entries)-1]
}

// Resolve evaluates the entry's templates against ctx and overrides, producing
// a SyslogResolved ready for the wire encoder. overrides, when non-nil,
// replace the corresponding fields in ctx — used by the on-demand HTTP
// endpoint to pin specific template values for CI/test-harness fires.
func (e *SyslogCatalogEntry) Resolve(ctx SyslogTemplateCtx, overrides map[string]string) (SyslogResolved, error) {
	if len(overrides) > 0 {
		if v, ok := overrides["DeviceIP"]; ok {
			ctx.DeviceIP = v
		}
		if v, ok := overrides["SysName"]; ok {
			ctx.SysName = v
		}
		if v, ok := overrides["IfIndex"]; ok {
			n, err := parseIntFieldSyslog(v, "IfIndex")
			if err != nil {
				return SyslogResolved{}, err
			}
			ctx.IfIndex = n
		}
		if v, ok := overrides["IfName"]; ok {
			ctx.IfName = v
		}
		if v, ok := overrides["Now"]; ok {
			n, err := parseIntFieldSyslog(v, "Now")
			if err != nil {
				return SyslogResolved{}, err
			}
			ctx.Now = int64(n)
		}
		if v, ok := overrides["Uptime"]; ok {
			n, err := parseIntFieldSyslog(v, "Uptime")
			if err != nil {
				return SyslogResolved{}, err
			}
			ctx.Uptime = uint32(n)
		}
		for k := range overrides {
			if _, ok := allowedSyslogTemplateFields[k]; !ok {
				return SyslogResolved{}, fmt.Errorf("syslog template override: unknown field %q", k)
			}
		}
	}
	return e.resolveAgainst(ctx, nil)
}

// resolveAgainst does the template-execute pass. It is separate from Resolve
// so the catalog-load MTU check can reuse it with the worst-case context
// without re-applying overrides.
func (e *SyslogCatalogEntry) resolveAgainst(ctx SyslogTemplateCtx, _ map[string]string) (SyslogResolved, error) {
	out := SyslogResolved{
		Facility: e.Facility,
		Severity: e.Severity,
		AppName:  e.AppName,
		MsgID:    e.MsgID,
	}
	var buf bytes.Buffer
	if e.TemplateTmpl != nil {
		buf.Reset()
		if err := e.TemplateTmpl.Execute(&buf, ctx); err != nil {
			return SyslogResolved{}, fmt.Errorf("syslog %q template: %w", e.Name, err)
		}
		out.Message = buf.String()
	}
	if e.HostnameTmpl != nil {
		buf.Reset()
		if err := e.HostnameTmpl.Execute(&buf, ctx); err != nil {
			return SyslogResolved{}, fmt.Errorf("syslog %q hostname: %w", e.Name, err)
		}
		out.Hostname = buf.String()
	}
	if len(e.StructuredDataKeys) > 0 {
		out.StructuredData = make([]SyslogSDPair, 0, len(e.StructuredDataKeys))
		for _, k := range e.StructuredDataKeys {
			raw := e.StructuredData[k]
			// Structured-data values may contain templates too.
			if strings.Contains(raw, "{{") {
				tmpl, err := template.New(e.Name + ".sd." + k).Parse(raw)
				if err != nil {
					return SyslogResolved{}, fmt.Errorf("syslog %q structuredData[%s]: %w", e.Name, k, err)
				}
				buf.Reset()
				if err := tmpl.Execute(&buf, ctx); err != nil {
					return SyslogResolved{}, fmt.Errorf("syslog %q structuredData[%s]: %w", e.Name, k, err)
				}
				out.StructuredData = append(out.StructuredData, SyslogSDPair{Key: k, Value: buf.String()})
			} else {
				out.StructuredData = append(out.StructuredData, SyslogSDPair{Key: k, Value: raw})
			}
		}
	}
	return out, nil
}

func parseIntFieldSyslog(s, name string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("syslog template override %s: expected integer, got %q", name, s)
	}
	return n, nil
}
