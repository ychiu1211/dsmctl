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

func TestDoThumbnailStreamsImageBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("method") != "get" || q.Get("path") != "/home/pic.png" || q.Get("size") != "large" || q.Get("rotate") != "2" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer server.Close()
	prep := newTransferPrep(t, server)
	content, err := doThumbnail(context.Background(), prep, prep.sid, prep.synoToken, "/home/pic.png", ThumbnailOptions{Size: "large", Rotate: 2})
	if err != nil {
		t.Fatalf("doThumbnail() error = %v", err)
	}
	defer content.Body.Close()
	if content.ContentType != "image/png" {
		t.Fatalf("content type = %q", content.ContentType)
	}
	data, _ := io.ReadAll(content.Body)
	if string(data) != "png-bytes" {
		t.Fatalf("body = %q", data)
	}
}

// TestThumbnailHTTPErrorRedactsCredentials pins the secrets contract: a non-2xx
// transfer error must never expose the _sid or SynoToken query parameters.
func TestThumbnailHTTPErrorRedactsCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()
	prep := newTransferPrep(t, server)
	_, err := doThumbnail(context.Background(), prep, "super-secret-sid", "super-secret-token", "/nope.png", ThumbnailOptions{})
	if err == nil {
		t.Fatal("expected an HTTP error")
	}
	if strings.Contains(err.Error(), "super-secret-sid") || strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("error leaked credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("error did not mark redaction: %v", err)
	}
}

func TestRedactTransferURLMasksSessionParameters(t *testing.T) {
	parsed, _ := url.Parse("https://nas.example:5001/webapi/entry.cgi?api=SYNO.FileStation.Thumb&_sid=abc123&SynoToken=tok999&path=%2Fx")
	got := redactTransferURL(*parsed)
	for _, secret := range []string{"abc123", "tok999"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted URL still contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "path=%2Fx") {
		t.Fatalf("redacted URL dropped a non-secret parameter: %s", got)
	}
}
