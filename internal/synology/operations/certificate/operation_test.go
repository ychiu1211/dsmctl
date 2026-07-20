package certificate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (e *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.request = request
	return e.response, nil
}

func certTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(CRTAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	return target
}

func TestSelectCertificatesRequiresAPI(t *testing.T) {
	if selection, err := SelectCertificates(certTarget()); err != nil || !selection.Supported || selection.Backend != "certificate-crt-list-v1" {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
	// Without the API advertised, the operation fails closed.
	if selection, err := SelectCertificates(compatibility.NewTarget()); !compatibility.IsUnsupported(err) || selection.Supported {
		t.Fatalf("selection=%#v err=%v", selection, err)
	}
}

func TestExecuteCertificatesDecodesLiveShape(t *testing.T) {
	// Response shape captured live from DSM 7.3 (SYNO.Core.Certificate.CRT list):
	// a self-signed default with bound services, and a renewable Let's Encrypt
	// cert with SANs. Private-key material is never present in the read.
	executor := &capturingExecutor{response: json.RawMessage(`{
		"certificates": [
			{"id":"PTItWZ","desc":"","is_default":true,"is_broken":false,"renewable":false,
			 "key_types":"","signature_algorithm":"sha256WithRSAEncryption",
			 "issuer":{"common_name":"Synology Inc. CA","country":"TW","organization":"Synology Inc."},
			 "subject":{"common_name":"synology","country":"TW","organization":"Synology Inc.","sub_alt_name":["synology"]},
			 "self_signed_cacrt_info":{"issuer":{"common_name":"Synology Inc. CA"}},
			 "valid_from":"Mar 15 15:49:37 2026 GMT","valid_till":"Mar 16 15:49:37 2027 GMT","user_deletable":true,
			 "services":[
			   {"service":"default","display_name":"DSM Desktop Service","display_name_i18n":"common:web_desktop","isPkg":false,"owner":"root","subscriber":"system"},
			   {"service":"ftpd","display_name":"FTPS","isPkg":false,"owner":"root","subscriber":"smbftpd"},
			   {"service":"SynologyDrive","display_name":"Synology Drive Server","isPkg":true,"owner":"SynologyDrive","subscriber":"SynologyDrive"}
			 ]},
			{"id":"VUDePf","desc":"Synology QuickConnect Certificate","is_default":false,"is_broken":false,"renewable":true,
			 "key_types":"RSA/ECC",
			 "issuer":{"common_name":"YR2","country":"US","organization":"Let's Encrypt"},
			 "subject":{"common_name":"derekchiu3018.direct.quickconnect.to","sub_alt_name":["*.derekchiu3018.direct.quickconnect.to","derekchiu3018.direct.quickconnect.to"]},
			 "valid_from":"Jul 14 11:51:08 2026 GMT","valid_till":"Oct 12 11:51:07 2026 GMT","user_deletable":true,
			 "services":[{"service":"quickconnect","display_name":"QuickConnect","isPkg":false,"owner":"root","subscriber":"system"}]}
		]
	}`)}
	certs, selection, err := ExecuteCertificates(context.Background(), certTarget(), executor)
	if err != nil {
		t.Fatalf("ExecuteCertificates() error = %v", err)
	}
	if executor.request.API != CRTAPIName || executor.request.Version != 1 || executor.request.Method != "list" {
		t.Fatalf("request = %#v", executor.request)
	}
	if selection.Backend != "certificate-crt-list-v1" || certs.Total != 2 || len(certs.Certificates) != 2 {
		t.Fatalf("certs = %#v selection = %#v", certs, selection)
	}
	def := certs.Certificates[0]
	if def.ID != "PTItWZ" || !def.IsDefault || !def.SelfSigned || def.Renewable {
		t.Fatalf("default cert = %#v", def)
	}
	if def.Subject.CommonName != "synology" || def.Issuer.Organization != "Synology Inc." {
		t.Fatalf("default names = %#v / %#v", def.Subject, def.Issuer)
	}
	if def.ValidTillUnix == 0 || def.ValidFromUnix == 0 {
		t.Fatalf("default validity not parsed: %d %d", def.ValidFromUnix, def.ValidTillUnix)
	}
	if len(def.Services) != 3 || def.Services[0].Service != "default" || def.Services[0].DisplayName != "DSM Desktop Service" || !def.Services[2].IsPackage {
		t.Fatalf("default services = %#v", def.Services)
	}
	le := certs.Certificates[1]
	if !le.Renewable || le.SelfSigned || le.KeyTypes != "RSA/ECC" || len(le.SubjectAltNames) != 2 {
		t.Fatalf("LE cert = %#v", le)
	}
	if le.Issuer.Organization != "Let's Encrypt" {
		t.Fatalf("LE issuer = %#v", le.Issuer)
	}
}

func TestExecuteCertificatesRejectsUnknownShape(t *testing.T) {
	executor := &capturingExecutor{response: json.RawMessage(`{"items":[]}`)}
	if _, _, err := ExecuteCertificates(context.Background(), certTarget(), executor); err == nil || !strings.Contains(err.Error(), "no certificate array") {
		t.Fatalf("error = %v", err)
	}
	executor = &capturingExecutor{response: json.RawMessage(`{"certificates":[{"desc":"no id"}]}`)}
	if _, _, err := ExecuteCertificates(context.Background(), certTarget(), executor); err == nil || !strings.Contains(err.Error(), "no id field") {
		t.Fatalf("missing-id error = %v", err)
	}
}
