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

// Syslog wire encoders. Two RFC-defined formats, one interface.
//
// Decision log:
//   - No trailing LF on UDP datagrams (design.md §OQ#1). Some legacy
//     syslog-ng configurations expect a terminator; operators hitting that
//     should file an issue and a -syslog-trailing-lf flag can be added.
//   - RFC 5424 BOM is OMITTED from the emitted MSG. RFC 5424 §6.4 makes it
//     optional; omitting keeps the wire bytes ASCII-clean and avoids the
//     "is this UTF-8 or Latin-1?" adventure downstream parsers have with
//     lone BOMs. Enable with a future flag if required.
//   - Timestamps are rendered in UTC. RFC 5424 §6.2.3 allows local offset
//     but UTC produces comparable timestamps across a 30k-device fleet.

package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// SyslogFormat identifies which wire encoder to use.
type SyslogFormat string

const (
	SyslogFormat5424 SyslogFormat = "5424"
	SyslogFormat3164 SyslogFormat = "3164"
)

// ParseSyslogFormat validates the operator-supplied -syslog-format value.
func ParseSyslogFormat(s string) (SyslogFormat, error) {
	switch s {
	case string(SyslogFormat5424):
		return SyslogFormat5424, nil
	case string(SyslogFormat3164):
		return SyslogFormat3164, nil
	default:
		return "", fmt.Errorf("invalid -syslog-format %q (valid: 5424, 3164)", s)
	}
}

// SyslogEncoder encodes one resolved catalog entry into its wire datagram.
// Implementations are stateless and safe for concurrent use.
type SyslogEncoder interface {
	// Encode appends the complete syslog datagram to buf. `now` is the
	// wall-clock time for the TIMESTAMP field. For RFC 3164, msg.Hostname
	// MUST be non-empty (the caller resolves the sysName / DeviceIP fallback
	// before calling); empty Hostname in RFC 5424 renders as NILVALUE `-`.
	Encode(buf *bytes.Buffer, msg SyslogResolved, now time.Time) error

	// MaxMessageSize returns the recommended upper bound on encoded
	// datagram bytes. Exporter write-buffers are sized against this.
	MaxMessageSize() int
}

// NewSyslogEncoder returns the encoder for the given format.
func NewSyslogEncoder(f SyslogFormat) (SyslogEncoder, error) {
	switch f {
	case SyslogFormat5424:
		return &RFC5424Encoder{}, nil
	case SyslogFormat3164:
		return &RFC3164Encoder{}, nil
	default:
		return nil, fmt.Errorf("unknown syslog format %q", f)
	}
}

// calculatePRI computes the RFC 5424 §6.2.1 PRI value as `<N>` with no
// leading zeros, where N = facility*8 + severity. Range: 0..191.
func calculatePRI(facility SyslogFacility, severity SyslogSeverity) string {
	// Bounds are already checked at catalog load; this is belt-and-braces.
	f := uint16(facility) & 0x1F
	s := uint16(severity) & 0x07
	return fmt.Sprintf("<%d>", f*8+s)
}

// sanitiseHostToken replaces whitespace with hyphens and rejects empty
// strings. Both RFC 3164 (§4.1.2) and RFC 5424 (§6.2.4) treat HOSTNAME as a
// single printable token; embedded whitespace breaks field parsing
// downstream. Caller pre-normalisation keeps the wire bytes predictable.
func sanitiseHostToken(s string) string {
	if s == "" {
		return ""
	}
	// Avoid allocating if there's nothing to do (common case).
	if strings.IndexAny(s, " \t\n\r") < 0 {
		return s
	}
	r := strings.NewReplacer(" ", "-", "\t", "-", "\n", "-", "\r", "-")
	return r.Replace(s)
}

// -----------------------------------------------------------------------------
// RFC 5424 encoder
// -----------------------------------------------------------------------------

// RFC5424Encoder emits structured syslog messages per RFC 5424 §6:
//
//	<PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA [BOM-MSG]
//
// Empty fields are rendered as the NILVALUE `-`. TIMESTAMP is ISO 8601 UTC
// with millisecond precision.
type RFC5424Encoder struct{}

// StructuredDataEnterpriseID is the IANA-assigned private enterprise number
// used in the SD-ID (e.g. `meta@32473`). 32473 is the reserved value from
// RFC 5612 for documentation / example use. Operators who want a real
// enterprise number should fork the encoder; this is a simulator, the SD-ID
// is cosmetic for downstream parsers.
const syslogSDEnterpriseID = 32473

// syslogSDID is the SD-ID used in emitted STRUCTURED-DATA elements. Kept as
// a single element because the v1 catalog schema ships a flat key/value map
// without per-key SD-ID partitioning.
const syslogSDID = "meta@32473"

// MaxMessageSize returns the MTU-safe encoding ceiling. Catalog entries are
// validated at load time against this bound (design.md §D12).
func (*RFC5424Encoder) MaxMessageSize() int { return maxSyslogMessageBytes }

func (*RFC5424Encoder) Encode(buf *bytes.Buffer, msg SyslogResolved, now time.Time) error {
	if buf == nil {
		return fmt.Errorf("syslog 5424: buf is nil")
	}
	// PRI + VERSION
	buf.WriteString(calculatePRI(msg.Facility, msg.Severity))
	buf.WriteByte('1')
	buf.WriteByte(' ')

	// TIMESTAMP: RFC 3339 with millisecond precision, UTC ('Z').
	// `time.RFC3339Nano` is too precise; build manually for ms.
	ts := now.UTC()
	fmt.Fprintf(buf, "%04d-%02d-%02dT%02d:%02d:%02d.%03dZ",
		ts.Year(), ts.Month(), ts.Day(),
		ts.Hour(), ts.Minute(), ts.Second(),
		ts.Nanosecond()/1_000_000,
	)
	buf.WriteByte(' ')

	// HOSTNAME / APP-NAME / PROCID / MSGID (NILVALUE `-` when empty).
	writeSyslog5424Field(buf, sanitiseHostToken(msg.Hostname))
	buf.WriteByte(' ')
	writeSyslog5424Field(buf, msg.AppName)
	buf.WriteByte(' ')
	// PROCID not yet in scope (design §D4); always NILVALUE.
	buf.WriteByte('-')
	buf.WriteByte(' ')
	writeSyslog5424Field(buf, msg.MsgID)
	buf.WriteByte(' ')

	// STRUCTURED-DATA.
	if len(msg.StructuredData) == 0 {
		buf.WriteByte('-')
	} else {
		buf.WriteByte('[')
		buf.WriteString(syslogSDID)
		for _, kv := range msg.StructuredData {
			buf.WriteByte(' ')
			buf.WriteString(kv.Key)
			buf.WriteString(`="`)
			// Escape backslash, double-quote, and close-bracket per RFC 5424
			// §6.3.3 (PARAM-VALUE) — these are the only reserved characters
			// inside the quoted value.
			writeSyslog5424SDParamValue(buf, kv.Value)
			buf.WriteByte('"')
		}
		buf.WriteByte(']')
	}

	// MSG (no BOM — decision at top of file).
	if msg.Message != "" {
		buf.WriteByte(' ')
		buf.WriteString(msg.Message)
	}
	return nil
}

// writeSyslog5424Field emits a single syslog 5424 header field value,
// substituting NILVALUE `-` for empty input.
func writeSyslog5424Field(buf *bytes.Buffer, v string) {
	if v == "" {
		buf.WriteByte('-')
		return
	}
	// Header fields are tokens (no spaces permitted per RFC 5424 ABNF). The
	// catalog's StringsLoader already rejects fields that contain spaces;
	// this is an additional guard for operator-supplied content.
	if strings.ContainsAny(v, " \t\n") {
		// Replace rather than error — at fire time we can't fail the write
		// and lose the message; substitute safer characters.
		v = strings.NewReplacer(" ", "_", "\t", "_", "\n", "_").Replace(v)
	}
	buf.WriteString(v)
}

// writeSyslog5424SDParamValue escapes reserved characters per RFC 5424
// §6.3.3. Inside PARAM-VALUE the characters `"`, `\`, and `]` must be
// backslash-escaped; all others pass through verbatim.
func writeSyslog5424SDParamValue(buf *bytes.Buffer, v string) {
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '"' || c == '\\' || c == ']' {
			buf.WriteByte('\\')
		}
		buf.WriteByte(c)
	}
}

// -----------------------------------------------------------------------------
// RFC 3164 encoder
// -----------------------------------------------------------------------------

// RFC3164Encoder emits BSD-style syslog messages per RFC 3164 §4:
//
//	<PRI>TIMESTAMP HOSTNAME TAG: MSG
//
// TIMESTAMP uses `Mmm DD HH:MM:SS` with a double-space pad between the
// month abbreviation and single-digit day-of-month values.
type RFC3164Encoder struct{}

// syslogTagMaxLen is the maximum TAG field length per RFC 3164 §5.3.
const syslogTagMaxLen = 32

// MaxMessageSize returns the MTU-safe encoding ceiling. 3164 is always
// smaller than 5424 for the same inputs, so the shared ceiling applies.
func (*RFC3164Encoder) MaxMessageSize() int { return maxSyslogMessageBytes }

func (*RFC3164Encoder) Encode(buf *bytes.Buffer, msg SyslogResolved, now time.Time) error {
	if buf == nil {
		return fmt.Errorf("syslog 3164: buf is nil")
	}
	if msg.Hostname == "" {
		return fmt.Errorf("syslog 3164: hostname is required")
	}
	// PRI
	buf.WriteString(calculatePRI(msg.Facility, msg.Severity))

	// TIMESTAMP — `Mmm DD HH:MM:SS` with two-space pad for single-digit DD.
	ts := now.UTC()
	day := ts.Day()
	if day < 10 {
		fmt.Fprintf(buf, "%s  %d %02d:%02d:%02d",
			ts.Month().String()[:3], day,
			ts.Hour(), ts.Minute(), ts.Second())
	} else {
		fmt.Fprintf(buf, "%s %d %02d:%02d:%02d",
			ts.Month().String()[:3], day,
			ts.Hour(), ts.Minute(), ts.Second())
	}
	buf.WriteByte(' ')

	// HOSTNAME
	buf.WriteString(sanitiseHostToken(msg.Hostname))
	buf.WriteByte(' ')

	// TAG — truncated to 32 characters. RFC 5424-only fields (msgId,
	// structuredData) are silently ignored (design.md Requirement
	// "RFC 3164 wire format").
	tag := msg.AppName
	if tag == "" {
		// 3164 has no NILVALUE; use a placeholder token.
		tag = "unknown"
	}
	if len(tag) > syslogTagMaxLen {
		tag = tag[:syslogTagMaxLen]
	}
	buf.WriteString(tag)
	buf.WriteByte(':')
	if msg.Message != "" {
		buf.WriteByte(' ')
		buf.WriteString(msg.Message)
	}
	return nil
}
