// Package kmip implements the independently selectable, read-only DSM operation
// for the Control Panel / Storage Manager KMIP (Key Management Interoperability
// Protocol) surface.
//
// One DSM API family is read: SYNO.Storage.CGI.KMIP v1 `get`, a single combined
// config API that reports both KMIP roles at once — whether this NAS runs a
// local KMIP server (holding keys for other Synology devices) and/or acts as a
// KMIP client (escrowing its own encrypted-share keys to an external server) —
// plus connection status and the bound certificate identities.
//
// Live-verified against the lab (DSM 7.3-81168, DS3018xs): the family is
// advertised (path entry.cgi, v1-1) and `get` returns the full shape even when
// the NAS reports support_kmip:"no". Every other candidate read method
// (list/query/info/status/list_client/list_key/test/...) returned code 103
// "method does not exist", confirming `get` is the sole read method and there is
// no separate escrowed-key or registered-client list to surface. The family
// lives under SYNO.Storage.CGI.* (Storage Manager), which is DSM-core — no
// package gates it (dsmctl package list shows none).
//
// SECRET HYGIENE: this module is read-only and never reads a private key,
// escrowed/wrapped key bytes, a pre-shared secret, a passphrase, or a client
// credential into any model; DSM does not return them on get, and the decoder
// reads an explicit non-secret whitelist only.
package kmip

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/kmip"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	// KMIPAPI is the live-verified family: SYNO.Storage.CGI.KMIP (NOT the
	// SYNO.Core.KMIP.* the spec guessed).
	KMIPAPI = "SYNO.Storage.CGI.KMIP"

	// ReadCapabilityName is the stable operation/capability name for the KMIP
	// status read, per the compatibility framework.
	ReadCapabilityName = "kmip.read"
)

// Input is the empty request the KMIP status read takes.
type Input struct{}

var statusOp = compatibility.Operation[Input, kmip.Status]{
	Name: ReadCapabilityName,
	Variants: []compatibility.Variant[Input, kmip.Status]{
		{
			Name: "storage-cgi-kmip-get-v1", API: KMIPAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(KMIPAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (kmip.Status, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: KMIPAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return kmip.Status{}, fmt.Errorf("call %s.get v1: %w", KMIPAPI, err)
				}
				return decodeStatus(data)
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting the backend.
func APINames() []string {
	return []string{KMIPAPI}
}

// Select reports the KMIP status backend selection without executing it.
func Select(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOp.Select(target)
	return selection, err
}

// ReadStatus reads the combined KMIP role/status. When the KMIP API family is
// absent the returned error satisfies compatibility.IsUnsupported (fail-closed):
// the caller reports the module (not supported) rather than fabricating an empty
// success. A NAS whose family is present but whose support_kmip is "no" still
// reads successfully and reports Supported:false — the honest not-configured
// view (this is what the lab returns).
func ReadStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (kmip.Status, compatibility.Selection, error) {
	return statusOp.Run(ctx, target, executor, Input{})
}
