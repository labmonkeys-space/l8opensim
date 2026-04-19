// Content tables for the l8opensim landing page.
// Kept as a plain module so it's cheap to tweak without touching components.

export type Feature = { icon: string; title: string; body: string };
export type Category = { name: string; items: string[] };
export type DocGroup = { group: string; body: string; links: { t: string; h: string }[] };
export type StatusGroup = { k: string; items: string[] };
export type TerminalLine =
  | { k: 'cmd' | 'out' | 'ok'; text: string }
  | { k: 'bar'; text: string };

export const FEATURES: Feature[] = [
  { icon: 'scale', title: '30,000 devices', body: 'Tested scale on a single host. Parallel TUN pre-allocation, lock-free sync.Map for O(1) OID lookups, pre-computed next-OID mappings.' },
  { icon: 'proto', title: 'Protocols', body: 'SNMP v2c/v3 (MD5/SHA1 · DES/AES128), SSH with VT100, HTTPS REST, NetFlow v5 / v9 / IPFIX. sFlow v5 (experimental).' },
  { icon: 'devices', title: '28 device types', body: 'Routers, switches, firewalls, servers, GPU servers (DGX/HGX), storage systems, Linux servers — across 8 categories.' },
  { icon: 'gpu', title: 'GPU simulation', body: 'NVIDIA DGX-A100 / H100 / HGX-H200 with per-GPU DCGM OIDs — utilization, VRAM, temp, power, fan, SM/memory clocks.' },
  { icon: 'isol', title: 'Namespace isolation', body: 'Each device runs in the dedicated opensim network namespace with its own TUN interface and IP.' },
  { icon: 'metric', title: 'Dynamic metrics', body: '100-point pre-generated sine-wave cycling for CPU, memory, temperature — correlated across related metrics.' },
];

export const CATEGORIES: Category[] = [
  { name: 'Core Routers',    items: ['Cisco ASR9K · 48', 'Cisco CRS-X · 144', 'Huawei NE8000 · 96', 'Nokia 7750 SR-12 · 72', 'Juniper MX960 · 96'] },
  { name: 'Edge Routers',    items: ['Juniper MX240 · 24', 'NEC IX3315 · 48', 'Cisco IOS · 4'] },
  { name: 'DC Switches',     items: ['Cisco Nexus 9500 · 48', 'Arista 7280R3 · 32'] },
  { name: 'Campus Switches', items: ['Cisco Catalyst 9500 · 48', 'Extreme VSP4450 · 48', 'D-Link DGS-3630 · 52'] },
  { name: 'Firewalls',       items: ['Palo Alto PA-3220 · 12', 'Fortinet FortiGate-600E · 20', 'SonicWall NSa 6700 · 16', 'Check Point 15600 · 24'] },
  { name: 'Servers',         items: ['Dell PowerEdge R750', 'HPE ProLiant DL380', 'IBM Power S922', 'Linux Server · Ubuntu 24.04'] },
  { name: 'GPU Servers',     items: ['NVIDIA DGX-A100 · 8×80GB', 'NVIDIA DGX-H100 · 8×80GB', 'NVIDIA HGX-H200 · 8×141GB'] },
  { name: 'Storage',         items: ['AWS S3', 'Pure Storage FlashArray', 'NetApp ONTAP', 'Dell EMC Unity'] },
];

// Paths match sidebars.ts / docs folder exactly.
export const DOCS: DocGroup[] = [
  { group: 'Getting Started', body: 'Build, bring up a small fleet, run in Docker.', links: [
    { t: 'Quick start', h: '/getting-started/quick-start' },
    { t: 'Docker',      h: '/getting-started/docker' },
  ]},
  { group: 'Operations', body: 'Scale to 30k, tune the opensim namespace, flow export and SNMP traps.', links: [
    { t: 'Scaling',           h: '/ops/scaling' },
    { t: 'Network namespace', h: '/ops/network-namespace' },
    { t: 'Flow export',       h: '/ops/flow-export' },
    { t: 'SNMP traps',        h: '/ops/snmp-traps' },
    { t: 'Troubleshooting',   h: '/ops/troubleshooting' },
  ]},
  { group: 'Reference', body: 'Architecture, CLI flags, REST API, device-type tables, protocol details.', links: [
    { t: 'Architecture',  h: '/reference/architecture' },
    { t: 'CLI flags',     h: '/reference/cli-flags' },
    { t: 'Web API',       h: '/reference/web-api' },
    { t: 'Device types',  h: '/reference/device-types' },
    { t: 'SNMP',          h: '/reference/snmp' },
    { t: 'SNMP traps',    h: '/reference/snmp-traps' },
    { t: 'Flow export',   h: '/reference/flow-export' },
    { t: 'Resource files',h: '/reference/resource-files' },
  ]},
  { group: 'GPU simulation', body: 'DGX/HGX simulation, DCGM OID layout, pollaris parser.', links: [
    { t: 'GPU overview',     h: '/reference/gpu' },
    { t: 'Protobuf model',   h: '/reference/gpu/proto-model' },
    { t: 'Pollaris & parsing', h: '/reference/gpu/pollaris' },
    { t: 'DCGM simulation',  h: '/reference/gpu/dcgm' },
  ]},
];

export const STATUS: StatusGroup[] = [
  { k: 'Stable',        items: ['SNMP v2c/v3', 'SSH (VT100)', 'HTTPS REST (storage)', 'NetFlow v5/v9/IPFIX', 'TUN + netns isolation', 'Web UI + REST API'] },
  { k: 'Experimental',  items: ['sFlow v5 (synthetic)', 'Layer 8 overlay'] },
  { k: 'Tested scale',  items: ['30,000 concurrent devices / host', '~50 MB base + ~1 KB / device', 'CPU: minimal in steady state'] },
];

export const TERMINAL_SCRIPT: TerminalLine[] = [
  { k: 'cmd', text: 'sudo ./simulator -auto-start-ip 10.0.0.1 -auto-count 30000' },
  { k: 'out', text: '[opensim] preflight: TUN module OK · file-descriptors 1048576' },
  { k: 'out', text: '[opensim] netns  opensim  created' },
  { k: 'out', text: '[opensim] parallel TUN pre-allocation  (workers=64)' },
  { k: 'bar', text: 'alloc' },
  { k: 'out', text: '[opensim] 30000/30000 interfaces up  in 12.4s' },
  { k: 'out', text: '[opensim] snmp v2c/v3 · ssh (vt100) · https rest · netflow v5/v9/ipfix' },
  { k: 'out', text: '[opensim] web ui   http://localhost:8080/' },
  { k: 'ok',  text: 'ready · 30000 devices · mem 77 MB · cpu 0.4%' },
  { k: 'cmd', text: 'snmpwalk -v2c -c public 10.0.0.1 1.3.6.1.2.1.1.1.0' },
  { k: 'out', text: 'SNMPv2-MIB::sysDescr.0 = Cisco IOS XR Software, ASR9K v7.9.2' },
];
