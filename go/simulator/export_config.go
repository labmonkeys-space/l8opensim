/*
 * © 2025 Sharon Aicler (saichler@gmail.com)
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 * You may obtain a copy of the License at:
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// Per-device export configuration DTOs.
//
// These structs are the JSON shape of the optional `flow`, `traps`, and
// `syslog` blocks in `POST /api/v1/devices`, and they double as the runtime
// state stored on each `DeviceSimulator`. A nil pointer means "this export
// type is disabled for this device".
//
// Validation and default-fill live here. The actual wiring to exporter
// lifecycles lands in phases 3–5 of the `per-device-export-config` change.

// jsonDuration is a time.Duration wrapper that marshals/unmarshals as a
// Go duration string ("10s", "5m", "1m30s") rather than a nanosecond
// integer. Operators write REST bodies by hand; integer nanoseconds are
// unreadable and conflict with the CLI flags that historically accepted
// seconds-as-integers.
type jsonDuration time.Duration

func (d jsonDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *jsonDuration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a JSON string like \"10s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = jsonDuration(parsed)
	return nil
}

// DeviceFlowConfig is the per-device flow-export configuration.
type DeviceFlowConfig struct {
	Collector       string       `json:"collector"`
	Protocol        string       `json:"protocol,omitempty"`
	TickInterval    jsonDuration `json:"tick_interval,omitempty"`
	ActiveTimeout   jsonDuration `json:"active_timeout,omitempty"`
	InactiveTimeout jsonDuration `json:"inactive_timeout,omitempty"`
}

// DeviceTrapConfig is the per-device SNMP trap/INFORM configuration.
type DeviceTrapConfig struct {
	Collector     string       `json:"collector"`
	Mode          string       `json:"mode,omitempty"`
	Community     string       `json:"community,omitempty"`
	Interval      jsonDuration `json:"interval,omitempty"`
	InformTimeout jsonDuration `json:"inform_timeout,omitempty"`
	InformRetries int          `json:"inform_retries,omitempty"`
}

// DeviceSyslogConfig is the per-device UDP syslog configuration.
type DeviceSyslogConfig struct {
	Collector string       `json:"collector"`
	Format    string       `json:"format,omitempty"`
	Interval  jsonDuration `json:"interval,omitempty"`
}

// Defaults applied by ApplyDefaults. Kept as named constants so PR2/3/4
// can reference the same values when constructing CLI-seed configs.
const (
	defaultFlowProtocol        = "netflow9"
	defaultFlowTickInterval    = 5 * time.Second
	defaultFlowActiveTimeout   = 30 * time.Second
	defaultFlowInactiveTimeout = 15 * time.Second

	defaultTrapMode          = "trap"
	defaultTrapCommunity     = "public"
	defaultTrapInterval      = 30 * time.Second
	defaultTrapInformTimeout = 5 * time.Second
	defaultTrapInformRetries = 2

	defaultSyslogFormat   = "5424"
	defaultSyslogInterval = 10 * time.Second
)

// ApplyDefaults fills in zero-valued fields with the simulator-wide
// defaults that historically came from the CLI flags. Safe to call on a
// nil receiver (no-op). Call this before Validate so the normalised,
// defaulted struct is what validation sees.
func (c *DeviceFlowConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.Protocol == "" {
		c.Protocol = defaultFlowProtocol
	}
	if time.Duration(c.TickInterval) == 0 {
		c.TickInterval = jsonDuration(defaultFlowTickInterval)
	}
	if time.Duration(c.ActiveTimeout) == 0 {
		c.ActiveTimeout = jsonDuration(defaultFlowActiveTimeout)
	}
	if time.Duration(c.InactiveTimeout) == 0 {
		c.InactiveTimeout = jsonDuration(defaultFlowInactiveTimeout)
	}
}

// Validate checks the config and canonicalises Protocol to its stable form
// (e.g. "nf9"/"NetFlow9" → "netflow9"). Safe to call on a nil receiver
// (no-op). Callers SHOULD invoke ApplyDefaults first.
func (c *DeviceFlowConfig) Validate() error {
	if c == nil {
		return nil
	}
	if err := validateCollector("flow", c.Collector); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(c.Protocol)) {
	case "netflow9", "nf9", "":
		c.Protocol = "netflow9"
	case "ipfix", "ipfix10":
		c.Protocol = "ipfix"
	case "netflow5", "nf5":
		c.Protocol = "netflow5"
	case "sflow", "sflow5":
		c.Protocol = "sflow"
	default:
		return fmt.Errorf("flow: invalid protocol %q (valid: netflow9, ipfix, netflow5, sflow)", c.Protocol)
	}
	if time.Duration(c.TickInterval) < 0 {
		return fmt.Errorf("flow: tick_interval must be >= 0, got %s", time.Duration(c.TickInterval))
	}
	if time.Duration(c.ActiveTimeout) < 0 {
		return fmt.Errorf("flow: active_timeout must be >= 0, got %s", time.Duration(c.ActiveTimeout))
	}
	if time.Duration(c.InactiveTimeout) < 0 {
		return fmt.Errorf("flow: inactive_timeout must be >= 0, got %s", time.Duration(c.InactiveTimeout))
	}
	return nil
}

// ApplyDefaults fills zero-valued fields. Safe on nil.
func (c *DeviceTrapConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.Mode == "" {
		c.Mode = defaultTrapMode
	}
	if c.Community == "" {
		c.Community = defaultTrapCommunity
	}
	if time.Duration(c.Interval) == 0 {
		c.Interval = jsonDuration(defaultTrapInterval)
	}
	if time.Duration(c.InformTimeout) == 0 {
		c.InformTimeout = jsonDuration(defaultTrapInformTimeout)
	}
	if c.InformRetries == 0 {
		c.InformRetries = defaultTrapInformRetries
	}
}

// Validate checks the config and canonicalises Mode. Uses ParseTrapMode
// (defined in trap_manager.go) for parity with the CLI-side accepted
// spelling rules. Safe on nil.
func (c *DeviceTrapConfig) Validate() error {
	if c == nil {
		return nil
	}
	if err := validateCollector("traps", c.Collector); err != nil {
		return err
	}
	mode, err := ParseTrapMode(c.Mode)
	if err != nil {
		return fmt.Errorf("traps: %w", err)
	}
	switch mode {
	case TrapModeTrap:
		c.Mode = "trap"
	case TrapModeInform:
		c.Mode = "inform"
	}
	if c.InformRetries < 0 {
		return fmt.Errorf("traps: inform_retries must be >= 0, got %d", c.InformRetries)
	}
	if time.Duration(c.Interval) < 0 {
		return fmt.Errorf("traps: interval must be >= 0, got %s", time.Duration(c.Interval))
	}
	if time.Duration(c.InformTimeout) < 0 {
		return fmt.Errorf("traps: inform_timeout must be >= 0, got %s", time.Duration(c.InformTimeout))
	}
	return nil
}

// ApplyDefaults fills zero-valued fields. Safe on nil.
func (c *DeviceSyslogConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.Format == "" {
		c.Format = defaultSyslogFormat
	}
	if time.Duration(c.Interval) == 0 {
		c.Interval = jsonDuration(defaultSyslogInterval)
	}
}

// Validate checks the config and canonicalises Format via
// ParseSyslogFormat (defined in syslog_wire.go). Safe on nil.
func (c *DeviceSyslogConfig) Validate() error {
	if c == nil {
		return nil
	}
	if err := validateCollector("syslog", c.Collector); err != nil {
		return err
	}
	fm, err := ParseSyslogFormat(c.Format)
	if err != nil {
		return fmt.Errorf("syslog: %w", err)
	}
	c.Format = string(fm)
	if time.Duration(c.Interval) < 0 {
		return fmt.Errorf("syslog: interval must be >= 0, got %s", time.Duration(c.Interval))
	}
	return nil
}

// validateCollector is the shared host:port + DNS-resolution check used
// by all three export configs. Empty string is rejected; any
// net.ResolveUDPAddr error (bad syntax, unresolvable host, unknown port)
// is wrapped with the subsystem name for easier diagnosis at the REST
// boundary.
func validateCollector(subsystem, collector string) error {
	if collector == "" {
		return fmt.Errorf("%s: collector is required", subsystem)
	}
	if _, err := net.ResolveUDPAddr("udp4", collector); err != nil {
		return fmt.Errorf("%s: collector %q: %w", subsystem, collector, err)
	}
	return nil
}

// Compile-time safety: ensure jsonDuration satisfies the json
// (Un)Marshaler interfaces. Catches accidental signature drift.
var (
	_ json.Marshaler   = jsonDuration(0)
	_ json.Unmarshaler = (*jsonDuration)(nil)
)
