package packagecenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// ServerAPIName is the online package-catalog API (Synology's repository).
const ServerAPIName = "SYNO.Core.Package.Server"

// CatalogCapabilityName reports whether the online catalog can be read.
const CatalogCapabilityName = "packagecenter.catalog.read"

var catalogOperation = compatibility.Operation[Input, packagecenter.Catalog]{
	Name: CatalogCapabilityName,
	Variants: []compatibility.Variant[Input, packagecenter.Catalog]{
		catalogVariant("core-package-server-v2", 2, 20),
		catalogVariant("core-package-server-v1", 1, 10),
	},
}

func catalogVariant(name string, version, priority int) compatibility.Variant[Input, packagecenter.Catalog] {
	return compatibility.Variant[Input, packagecenter.Catalog]{
		Name: name, API: ServerAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(ServerAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (packagecenter.Catalog, error) {
			// blloadothers=false requests Synology-published packages only.
			data, err := executor.Execute(ctx, compatibility.Request{
				API: ServerAPIName, Version: version, Method: "list",
				JSONParameters: map[string]any{"blloadothers": false, "blforcereload": false},
			})
			if err != nil {
				return packagecenter.Catalog{}, fmt.Errorf("call %s.list v%d: %w", ServerAPIName, version, err)
			}
			return decodeCatalog(data)
		},
	}
}

func SelectCatalog(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := catalogOperation.Select(target)
	return selection, err
}

func ExecuteCatalog(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (packagecenter.Catalog, compatibility.Selection, error) {
	return catalogOperation.Run(ctx, target, executor, Input{})
}

// decodeCatalog reads the online-server list. The exact field names vary across
// DSM builds, so it tolerantly accepts common spellings for the download
// metadata; a raw dump is available under DSMCTL_DUMP for live confirmation.
func decodeCatalog(data json.RawMessage) (packagecenter.Catalog, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return packagecenter.Catalog{}, fmt.Errorf("decode package catalog: expected a non-empty object")
	}
	// DSM returns stable packages under "packages" and beta builds under
	// "beta_packages"; both share the same per-package shape.
	var envelope struct {
		Packages     []map[string]json.RawMessage `json:"packages"`
		BetaPackages []map[string]json.RawMessage `json:"beta_packages"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return packagecenter.Catalog{}, fmt.Errorf("decode package catalog: %w", err)
	}
	catalog := packagecenter.Catalog{Packages: make([]packagecenter.AvailablePackage, 0, len(envelope.Packages)+len(envelope.BetaPackages))}
	for _, raw := range envelope.Packages {
		catalog.Packages = append(catalog.Packages, decodeAvailablePackage(raw))
	}
	for _, raw := range envelope.BetaPackages {
		pkg := decodeAvailablePackage(raw)
		pkg.Beta = true
		catalog.Packages = append(catalog.Packages, pkg)
	}
	return catalog, nil
}

func decodeAvailablePackage(raw map[string]json.RawMessage) packagecenter.AvailablePackage {
	return packagecenter.AvailablePackage{
		ID:           firstString(raw, "id", "package"),
		Name:         firstString(raw, "dname", "name"),
		Version:      firstString(raw, "version", "dsm_version"),
		DownloadLink: firstString(raw, "link", "dlpath", "download_link"),
		Checksum:     firstString(raw, "md5", "checksum"),
		Beta:         firstBool(raw, "beta"),
		Size:         firstInt(raw, "size", "filesize", "download_size"),
		QuickInstall: firstBool(raw, "qinst"),
	}
}

func firstString(raw map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var text string
			if err := json.Unmarshal(value, &text); err == nil && text != "" {
				return text
			}
		}
	}
	return ""
}

func firstBool(raw map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var b bool
			if err := json.Unmarshal(value, &b); err == nil {
				return b
			}
		}
	}
	return false
}

func firstInt(raw map[string]json.RawMessage, names ...string) int64 {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var n int64
			if err := json.Unmarshal(value, &n); err == nil {
				return n
			}
			var text string
			if err := json.Unmarshal(value, &text); err == nil && text != "" {
				var parsed int64
				if _, scanErr := fmt.Sscan(text, &parsed); scanErr == nil {
					return parsed
				}
			}
		}
	}
	return 0
}
