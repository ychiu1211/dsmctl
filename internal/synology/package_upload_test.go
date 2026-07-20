package synology

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	pkgops "github.com/ychiu1211/dsmctl/internal/synology/operations/packagecenter"
)

// TestPackageUploadMultipartContract drives the shared streaming multipart
// transport with exactly the field set uploadPackageLocked builds for a local
// .spk install, and confirms the request DSM receives is well formed: every
// parameter field precedes the file part, the file travels under the "file"
// field with the .spk name, the `additional` field asks for the INFO keys, and
// the realistic upload envelope decodes to the package identity used to confirm
// the install. It captures the request instead of touching a NAS.
func TestPackageUploadMultipartContract(t *testing.T) {
	const version = 1
	var (
		gotFields   = map[string]string{}
		gotFileName string
		gotFileBody string
		fileWasLast bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("content type = %q, %v", mediaType, err)
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		sawFile := false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("next part: %v", err)
				break
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == pkgops.UploadFileField {
				sawFile = true
				gotFileName = part.FileName()
				gotFileBody = string(data)
				fileWasLast = true
			} else {
				if sawFile {
					// A parameter appeared after the file part.
					fileWasLast = false
				}
				gotFields[part.FormName()] = string(data)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// Shape mirrors DSM's SYNO.Core.Package.Installation.upload response: a
		// temp reference plus the INFO parsed from the uploaded .spk.
		_, _ = io.WriteString(w, `{"success":true,"data":{"task_id":"@SYNOPKG_UPLOAD_x","filename":"/tmp/pkg.spk","id":"dsmctl-gateway","name":"dsmctl Gateway","version":"0.1.0-1"}}`)
	}))
	defer server.Close()

	prep := newTransferPrep(t, server)
	prep.apiPath = "entry.cgi"
	fields := []multipartField{
		{"api", pkgops.InstallationAPIName},
		{"version", strconv.Itoa(version)},
		{"method", pkgops.UploadMethod},
		{pkgops.UploadAdditionalField, pkgops.UploadAdditionalValue},
		{"_sid", prep.sid},
		{"SynoToken", prep.synoToken},
	}
	raw, err := doMultipartUpload(context.Background(), prep, prep.sid, prep.synoToken,
		pkgops.InstallationAPIName, pkgops.UploadMethod, fields,
		pkgops.UploadFileField, "pkg.spk", strings.NewReader("fake spk bytes"), int64(len("fake spk bytes")))
	if err != nil {
		t.Fatalf("doMultipartUpload() error = %v", err)
	}

	if !fileWasLast {
		t.Error("a parameter field followed the file part; DSM requires every field before the file")
	}
	if gotFileName != "pkg.spk" || gotFileBody != "fake spk bytes" {
		t.Fatalf("file part = name %q body %q", gotFileName, gotFileBody)
	}
	if gotFields["api"] != pkgops.InstallationAPIName || gotFields["method"] != pkgops.UploadMethod ||
		gotFields["version"] != strconv.Itoa(version) {
		t.Fatalf("routing fields = %#v", gotFields)
	}
	if gotFields[pkgops.UploadAdditionalField] != pkgops.UploadAdditionalValue {
		t.Fatalf("additional field = %q, want the INFO key list", gotFields[pkgops.UploadAdditionalField])
	}
	if gotFields["_sid"] != prep.sid || gotFields["SynoToken"] != prep.synoToken {
		t.Fatalf("auth fields = _sid %q SynoToken %q", gotFields["_sid"], gotFields["SynoToken"])
	}

	upload, err := pkgops.DecodeUploadResult(raw)
	if err != nil {
		t.Fatalf("DecodeUploadResult() error = %v", err)
	}
	if upload.TaskID != "@SYNOPKG_UPLOAD_x" || upload.PackageID != "dsmctl-gateway" || upload.Version != "0.1.0-1" {
		t.Fatalf("decoded upload = %#v", upload)
	}
}
