// Package certificate implements the read-only DSM operation for the Control
// Panel → Security → Certificate surface. Certificate management is DSM core
// (not a package), so selection uses the advertised API/version alone.
//
// Verified live on DSM 7.3 (DS3018xs): SYNO.Core.Certificate.CRT v1 `list`
// returns each certificate's public metadata plus the services bound to it, so
// one read covers both the inventory and the bindings. The guarded writes
// (CRT set/delete/renew and the multipart import) are deferred to a later,
// explicitly-authorized slice — replacing or deleting the DSM certificate can
// break admin TLS.
package certificate

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	CRTAPIName = "SYNO.Core.Certificate.CRT"

	CertificatesReadCapabilityName = "certificate.certificates.read"
)

// Input is the empty input for the parameterless read.
type Input struct{}

var certificatesOperation = compatibility.Operation[Input, certificate.Certificates]{
	Name: CertificatesReadCapabilityName,
	Variants: []compatibility.Variant[Input, certificate.Certificates]{
		{
			Name: "certificate-crt-list-v1", API: CRTAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(CRTAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (certificate.Certificates, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: CRTAPIName, Version: 1, Method: "list"})
				if err != nil {
					return certificate.Certificates{}, err
				}
				return decodeCertificates(data)
			},
		},
	},
}

// APINames lists every DSM API this module may use so the facade can discover
// them in one call before selecting variants.
func APINames() []string {
	return []string{CRTAPIName}
}

func SelectCertificates(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := certificatesOperation.Select(target)
	return selection, err
}

func ExecuteCertificates(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (certificate.Certificates, compatibility.Selection, error) {
	return certificatesOperation.Run(ctx, target, executor, Input{})
}
