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

//go:build !linux

// Non-Linux stubs for TUN interface management.
// The real implementations require Linux kernel TUN/TAP and ioctl.
package main

import (
	"net"
	"strings"
)

func createTunInterface(name string, ip net.IP, netmask string) (*TunInterface, error) {
	return nil, errNotLinux
}

func createTunInterfaceInNamespaceViaExec(nsName, tunName string, ip net.IP, netmask string) (*TunInterface, error) {
	return nil, errNotLinux
}

func (tun *TunInterface) destroy() error { return nil }

// compareOIDsLexicographically compares two OID strings numerically component
// by component. Returns negative / zero / positive like strings.Compare.
// This implementation is portable and identical to the one in tun.go.
func compareOIDsLexicographically(oid1, oid2 string) int {
	parts1 := strings.Split(strings.TrimPrefix(oid1, "."), ".")
	parts2 := strings.Split(strings.TrimPrefix(oid2, "."), ".")

	minLen := len(parts1)
	if len(parts2) < minLen {
		minLen = len(parts2)
	}

	for i := 0; i < minLen; i++ {
		n1 := parseOIDComponent(parts1[i])
		n2 := parseOIDComponent(parts2[i])
		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	if len(parts1) < len(parts2) {
		return -1
	}
	if len(parts1) > len(parts2) {
		return 1
	}
	return 0
}

func parseOIDComponent(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
