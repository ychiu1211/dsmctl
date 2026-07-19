package synology

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTransferPrep(t *testing.T, server *httptest.Server) transferPrep {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return transferPrep{
		endpoint:  *parsed,
		apiPath:   "entry.cgi",
		version:   2,
		sid:       "test-sid",
		synoToken: "test-token",
		client:    server.Client(),
	}
}

func TestDoUploadSendsParametersBeforeFile(t *testing.T) {
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
			if part.FormName() == "file" {
				sawFile = true
				gotFileName = part.FileName()
				gotFileBody = string(data)
				// No further parts may follow the file part.
				fileWasLast = true
			} else {
				if sawFile {
					fileWasLast = false // a parameter appeared after the file
				}
				gotFields[part.FormName()] = string(data)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"blSkip":false,"file":"note.txt"}}`)
	}))
	defer server.Close()

	prep := newTransferPrep(t, server)
	result, err := doUpload(context.Background(), prep, prep.sid, prep.synoToken, "/home/dir", "note.txt",
		strings.NewReader("hello filestation"), int64(len("hello filestation")),
		UploadOptions{CreateParents: true, Overwrite: true})
	if err != nil {
		t.Fatalf("doUpload() error = %v", err)
	}
	if result.File != "note.txt" || result.Skipped {
		t.Fatalf("result = %#v", result)
	}
	if !fileWasLast {
		t.Fatalf("the file part must be sent after every parameter field")
	}
	for _, name := range []string{"api", "version", "method", "path", "create_parents", "overwrite", "_sid"} {
		if _, ok := gotFields[name]; !ok {
			t.Fatalf("missing multipart field %q; got %#v", name, gotFields)
		}
	}
	if gotFields["method"] != "upload" || gotFields["path"] != "/home/dir" || gotFields["overwrite"] != "true" || gotFields["_sid"] != "test-sid" {
		t.Fatalf("fields = %#v", gotFields)
	}
	if gotFileName != "note.txt" || gotFileBody != "hello filestation" {
		t.Fatalf("file name = %q body = %q", gotFileName, gotFileBody)
	}
}

func TestDoUploadAPIErrorSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":false,"error":{"code":414}}`)
	}))
	defer server.Close()
	prep := newTransferPrep(t, server)
	_, err := doUpload(context.Background(), prep, prep.sid, prep.synoToken, "/home", "x", strings.NewReader("x"), 1, UploadOptions{})
	var apiErr *APIError
	if err == nil || !strings.Contains(err.Error(), "414") {
		t.Fatalf("expected API error code 414, got %v", err)
	}
	_ = apiErr
}

func TestDoDownloadStreamsBinaryWithFilename(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("method") != "download" || r.URL.Query().Get("path") != "/home/file.bin" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="file.bin"`)
		_, _ = w.Write([]byte("binary-bytes"))
	}))
	defer server.Close()
	prep := newTransferPrep(t, server)
	content, err := doDownload(context.Background(), prep, prep.sid, prep.synoToken, "/home/file.bin")
	if err != nil {
		t.Fatalf("doDownload() error = %v", err)
	}
	defer content.Body.Close()
	if content.Filename != "file.bin" {
		t.Fatalf("filename = %q", content.Filename)
	}
	data, _ := io.ReadAll(content.Body)
	if string(data) != "binary-bytes" {
		t.Fatalf("body = %q", data)
	}
}

func TestDoDownloadJSONErrorBecomesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":false,"error":{"code":408}}`)
	}))
	defer server.Close()
	prep := newTransferPrep(t, server)
	_, err := doDownload(context.Background(), prep, prep.sid, prep.synoToken, "/nope")
	if err == nil || !strings.Contains(err.Error(), "408") {
		t.Fatalf("expected API error code 408, got %v", err)
	}
}
