// Package runtime provides the embedded VPN orchestration script.
package runtime

import _ "embed"

// Script is executed in Bash without being extracted to disk.
//
//go:embed rwth-vpn.sh
var Script string
