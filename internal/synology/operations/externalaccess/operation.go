// Package externalaccess implements independently selectable, read-only DSM
// operations for the Control Panel → External Access surface: the Synology
// Account (MyDS) binding, QuickConnect, and DDNS. Each area reads a distinct DSM
// API family; an area whose API is absent is reported unsupported without
// affecting the others.
package externalaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/derekvery666/dsmctl/internal/domain/externalaccess"
	"github.com/derekvery666/dsmctl/internal/synology/compatibility"
)

const (
	MyDSCenterAPI       = "SYNO.Core.MyDSCenter"
	PackageMyDSAPI      = "SYNO.Core.Package.MyDS"
	QuickConnectAPI     = "SYNO.Core.QuickConnect"
	QuickConnectPermAPI = "SYNO.Core.QuickConnect.Permission"
	DDNSRecordAPI       = "SYNO.Core.DDNS.Record"
	DDNSExtIPAPI        = "SYNO.Core.DDNS.ExtIP"
	PortForwardRulesAPI = "SYNO.Core.PortForwarding.Rules"
	PortForwardConfAPI  = "SYNO.Core.PortForwarding.RouterConf"

	AccountReadCapabilityName          = "externalaccess.account.read"
	QuickConnectReadCapabilityName     = "externalaccess.quickconnect.read"
	QuickConnectRelaySetCapabilityName = "externalaccess.quickconnect.relay.set"
	DDNSReadCapabilityName             = "externalaccess.ddns.read"
	PortForwardReadCapabilityName      = "externalaccess.portforward.read"
)

// QuickConnectMutationResult records the DSM backend that accepted a
// QuickConnect change.
type QuickConnectMutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

// Input is the empty request every read operation takes.
type Input struct{}

// Intermediate decoded partials, composed into the domain state by the Read
// functions below.
type accountCore struct {
	LoggedIn  bool
	Activated bool
	Account   string
}

type accountPackage struct {
	MyDSID string
	Serial string
}

type quickConnectConfig struct {
	Enabled      bool
	ID           string
	Region       string
	Domain       string
	DirectDomain string
}

type quickConnectRelay struct {
	RelayEnabled bool
}

type quickConnectStatus struct {
	ConnectionStatus string
	AliasStatus      string
}

type ddnsRecords struct {
	Records        []externalaccess.DDNSRecord
	NextUpdateTime string
}

func readVariant[O any](name, api string, version, priority int, method string, decode func(json.RawMessage) (O, error)) compatibility.Variant[Input, O] {
	return compatibility.Variant[Input, O]{
		Name:     name,
		API:      api,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(api, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (O, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: method})
			if err != nil {
				var zero O
				return zero, fmt.Errorf("call %s.%s v%d: %w", api, method, version, err)
			}
			return decode(data)
		},
	}
}

var accountCoreOp = compatibility.Operation[Input, accountCore]{
	Name: "externalaccess.account.core.read",
	Variants: []compatibility.Variant[Input, accountCore]{
		readVariant("myds-center-query-v2", MyDSCenterAPI, 2, 20, "query", decodeAccountCore),
		readVariant("myds-center-query-v1", MyDSCenterAPI, 1, 10, "query", decodeAccountCore),
	},
}

var accountPackageOp = compatibility.Operation[Input, accountPackage]{
	Name: "externalaccess.account.package.read",
	Variants: []compatibility.Variant[Input, accountPackage]{
		readVariant("package-myds-get-v1", PackageMyDSAPI, 1, 10, "get", decodeAccountPackage),
	},
}

var quickConnectConfigOp = compatibility.Operation[Input, quickConnectConfig]{
	Name: "externalaccess.quickconnect.config.read",
	Variants: []compatibility.Variant[Input, quickConnectConfig]{
		readVariant("quickconnect-get-v2", QuickConnectAPI, 2, 20, "get", decodeQuickConnectConfig),
		readVariant("quickconnect-get-v1", QuickConnectAPI, 1, 10, "get", decodeQuickConnectConfig),
	},
}

var quickConnectRelayOp = compatibility.Operation[Input, quickConnectRelay]{
	Name: "externalaccess.quickconnect.relay.read",
	Variants: []compatibility.Variant[Input, quickConnectRelay]{
		readVariant("quickconnect-relay-v3", QuickConnectAPI, 3, 30, "get_misc_config", decodeQuickConnectRelay),
	},
}

var quickConnectStatusOp = compatibility.Operation[Input, quickConnectStatus]{
	Name: "externalaccess.quickconnect.status.read",
	Variants: []compatibility.Variant[Input, quickConnectStatus]{
		readVariant("quickconnect-status-v1", QuickConnectAPI, 1, 10, "status", decodeQuickConnectStatus),
	},
}

var quickConnectPermissionOp = compatibility.Operation[Input, []externalaccess.QuickConnectService]{
	Name: "externalaccess.quickconnect.permission.read",
	Variants: []compatibility.Variant[Input, []externalaccess.QuickConnectService]{
		readVariant("quickconnect-permission-get-v1", QuickConnectPermAPI, 1, 10, "get", decodeQuickConnectPermission),
	},
}

var ddnsRecordOp = compatibility.Operation[Input, ddnsRecords]{
	Name: "externalaccess.ddns.record.read",
	Variants: []compatibility.Variant[Input, ddnsRecords]{
		{
			Name:     "ddns-record-list-v1",
			API:      DDNSRecordAPI,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(DDNSRecordAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (ddnsRecords, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: DDNSRecordAPI, Version: 1, Method: "list"})
				if err != nil {
					return ddnsRecords{}, fmt.Errorf("call %s.list v1: %w", DDNSRecordAPI, err)
				}
				records, nextUpdate, err := decodeDDNSRecords(data)
				if err != nil {
					return ddnsRecords{}, err
				}
				return ddnsRecords{Records: records, NextUpdateTime: nextUpdate}, nil
			},
		},
	},
}

var ddnsExtIPOp = compatibility.Operation[Input, []externalaccess.ExternalAddress]{
	Name: "externalaccess.ddns.extip.read",
	Variants: []compatibility.Variant[Input, []externalaccess.ExternalAddress]{
		readVariant("ddns-extip-list-v2", DDNSExtIPAPI, 2, 20, "list", decodeDDNSExternalAddresses),
		readVariant("ddns-extip-list-v1", DDNSExtIPAPI, 1, 10, "list", decodeDDNSExternalAddresses),
	},
}

var portForwardRulesOp = compatibility.Operation[Input, []externalaccess.PortForwardRule]{
	Name: "externalaccess.portforward.rules.read",
	Variants: []compatibility.Variant[Input, []externalaccess.PortForwardRule]{
		readVariant("portforwarding-rules-load-v1", PortForwardRulesAPI, 1, 10, "load", decodePortForwardRules),
	},
}

var portForwardRouterOp = compatibility.Operation[Input, externalaccess.PortForwardRouter]{
	Name: "externalaccess.portforward.router.read",
	Variants: []compatibility.Variant[Input, externalaccess.PortForwardRouter]{
		readVariant("portforwarding-routerconf-get-v1", PortForwardConfAPI, 1, 10, "get", decodeRouterConf),
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	unique := map[string]struct{}{}
	for _, names := range [][]string{
		accountCoreOp.APINames(), accountPackageOp.APINames(),
		quickConnectConfigOp.APINames(), quickConnectRelayOp.APINames(),
		quickConnectStatusOp.APINames(), quickConnectPermissionOp.APINames(),
		ddnsRecordOp.APINames(), ddnsExtIPOp.APINames(),
		portForwardRulesOp.APINames(), portForwardRouterOp.APINames(),
	} {
		for _, name := range names {
			unique[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// SelectAccount, SelectQuickConnect, and SelectDDNS report each area's primary
// read selection, so capabilities can be described without a read.
func SelectAccount(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := accountCoreOp.Select(target)
	return selection, err
}

func SelectQuickConnect(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := quickConnectConfigOp.Select(target)
	return selection, err
}

func SelectDDNS(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ddnsRecordOp.Select(target)
	return selection, err
}

func SelectPortForward(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := portForwardRulesOp.Select(target)
	return selection, err
}

// relaySetMethod is the wire method that writes the relay toggle. It is the
// symmetric setter of the get_misc_config read (both take/return relay_enabled);
// the conf's "set_relay_enable" is only the Python spec variable name and DSM
// answers it with code 103 (live-verified on DSM 7.3).
const relaySetMethod = "set_misc_config"

// quickConnectRelaySetOp writes only the relay toggle, via v3 set_misc_config
// (param live-verified: relay_enabled). Older QuickConnect versions do not
// expose the relay setting and fail closed before any request.
var quickConnectRelaySetOp = compatibility.Operation[bool, QuickConnectMutationResult]{
	Name: "externalaccess.quickconnect.relay.set",
	Variants: []compatibility.Variant[bool, QuickConnectMutationResult]{
		{
			Name: "quickconnect-set-relay-v3", API: QuickConnectAPI, Version: 3, Priority: 30,
			Match: compatibility.APIVersion(QuickConnectAPI, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, enabled bool) (QuickConnectMutationResult, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: QuickConnectAPI, Version: 3, Method: relaySetMethod,
					JSONParameters: map[string]any{"relay_enabled": enabled},
				}); err != nil {
					return QuickConnectMutationResult{}, fmt.Errorf("call %s.%s v3: %w", QuickConnectAPI, relaySetMethod, err)
				}
				return QuickConnectMutationResult{}, nil
			},
		},
	},
}

func SelectQuickConnectRelaySet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := quickConnectRelaySetOp.Select(target)
	return selection, err
}

// ExecuteQuickConnectRelaySet writes the relay toggle. The caller must have
// confirmed the change differs from the current state while planning.
func ExecuteQuickConnectRelaySet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, enabled bool) (QuickConnectMutationResult, compatibility.Selection, error) {
	result, selection, err := quickConnectRelaySetOp.Run(ctx, target, executor, enabled)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, relaySetMethod
	}
	return result, selection, err
}

// ReadAccount reads the Synology Account binding. The MyDSCenter query is
// required; the package read enriches it with the customer id and serial and is
// skipped when its API is absent or no account is currently logged in.
func ReadAccount(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externalaccess.AccountState, compatibility.Selection, error) {
	core, selection, err := accountCoreOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externalaccess.AccountState{}, selection, err
	}
	state := externalaccess.AccountState{LoggedIn: core.LoggedIn, Activated: core.Activated, Account: core.Account}
	if core.LoggedIn {
		if pkg, ok, err := runOptional(ctx, target, executor, accountPackageOp); err != nil {
			return externalaccess.AccountState{}, selection, err
		} else if ok {
			state.MyDSID = pkg.MyDSID
			state.Serial = pkg.Serial
		}
	}
	return state, selection, nil
}

// ReadQuickConnect reads the QuickConnect configuration (required) and enriches
// it with the relay setting, live status, and per-service permission when those
// independently versioned APIs are available.
func ReadQuickConnect(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externalaccess.QuickConnectState, compatibility.Selection, error) {
	config, selection, err := quickConnectConfigOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externalaccess.QuickConnectState{}, selection, err
	}
	state := externalaccess.QuickConnectState{
		Enabled:      config.Enabled,
		ID:           config.ID,
		Region:       config.Region,
		Domain:       config.Domain,
		DirectDomain: config.DirectDomain,
	}
	if relay, ok, err := runOptional(ctx, target, executor, quickConnectRelayOp); err != nil {
		return externalaccess.QuickConnectState{}, selection, err
	} else if ok {
		enabled := relay.RelayEnabled
		state.RelayEnabled = &enabled
	}
	if status, ok, err := runOptional(ctx, target, executor, quickConnectStatusOp); err != nil {
		return externalaccess.QuickConnectState{}, selection, err
	} else if ok {
		state.ConnectionStatus = status.ConnectionStatus
		state.AliasStatus = status.AliasStatus
	}
	if services, ok, err := runOptional(ctx, target, executor, quickConnectPermissionOp); err != nil {
		return externalaccess.QuickConnectState{}, selection, err
	} else if ok {
		state.Services = services
	}
	return state, selection, nil
}

// ReadDDNS reads the configured DDNS records (required) and the detected WAN
// addresses (skipped when the external-IP API is absent).
func ReadDDNS(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externalaccess.DDNSState, compatibility.Selection, error) {
	records, selection, err := ddnsRecordOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externalaccess.DDNSState{}, selection, err
	}
	state := externalaccess.DDNSState{Records: records.Records, NextUpdateTime: records.NextUpdateTime, ExternalAddress: []externalaccess.ExternalAddress{}}
	if addresses, ok, err := runOptional(ctx, target, executor, ddnsExtIPOp); err != nil {
		return externalaccess.DDNSState{}, selection, err
	} else if ok {
		state.ExternalAddress = addresses
	}
	return state, selection, nil
}

// ReadPortForward reads the configured port-forwarding rules (required) and the
// paired router configuration (skipped when its API is absent).
func ReadPortForward(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externalaccess.PortForwardState, compatibility.Selection, error) {
	rules, selection, err := portForwardRulesOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externalaccess.PortForwardState{}, selection, err
	}
	state := externalaccess.PortForwardState{Rules: rules}
	if router, ok, err := runOptional(ctx, target, executor, portForwardRouterOp); err != nil {
		return externalaccess.PortForwardState{}, selection, err
	} else if ok {
		state.Router = router
	}
	return state, selection, nil
}

// runOptional runs an enrichment operation only when its API is available. An
// unsupported operation is a normal "skip" (ok=false, nil error); any other
// selection or execution failure is returned.
func runOptional[O any](ctx context.Context, target compatibility.Target, executor compatibility.Executor, operation compatibility.Operation[Input, O]) (O, bool, error) {
	var zero O
	if _, selection, err := operation.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return zero, false, nil
		}
		return zero, false, err
	} else if !selection.Supported {
		return zero, false, nil
	}
	result, _, err := operation.Run(ctx, target, executor, Input{})
	if err != nil {
		return zero, false, err
	}
	return result, true, nil
}
