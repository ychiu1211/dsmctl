package synology

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	filestationops "github.com/ychiu1211/dsmctl/internal/synology/operations/filestation"
)

// UploadOptions controls how a file is written to the NAS.
type UploadOptions struct {
	// CreateParents creates missing parent folders of the destination.
	CreateParents bool
	// Overwrite replaces an existing destination file; when false DSM rejects an
	// upload whose destination already exists.
	Overwrite bool
}

// UploadResult is the normalized outcome of a FileStation upload.
type UploadResult struct {
	File    string `json:"file,omitempty" jsonschema:"Destination file name echoed by DSM"`
	Skipped bool   `json:"skipped,omitempty" jsonschema:"Whether DSM skipped the upload because the destination already existed"`
}

// DownloadContent is a streaming download. Body is live and unbuffered — the
// caller MUST Close it. Size is the Content-Length, or -1 when DSM does not
// report one (for example a folder DSM zips on the fly).
type DownloadContent struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
	Filename    string
}

// streamingClient returns an HTTP client for binary transfers. It shares the
// configured (pinned-TLS) transport but drops the fixed per-request timeout, so
// a large upload or download is bounded only by the caller's context rather than
// the 30-second default that fits JSON calls.
func (c *Client) streamingClient() *http.Client {
	clone := *c.httpClient
	clone.Timeout = 0
	return &clone
}

// transferPrep captures, under the client mutex, everything a binary transfer
// needs so the transfer itself runs without holding the lock.
type transferPrep struct {
	endpoint  url.URL
	apiPath   string
	version   int
	sid       string
	synoToken string
	client    *http.Client
}

// prepareTransferLocked discovers the FileStation APIs, selects the requested
// transfer backend, ensures a session, records the capability, and captures the
// immutable request inputs. It runs entirely under c.mu; the caller unlocks
// before streaming.
func (c *Client) prepareTransferLocked(ctx context.Context, apiName, capabilityName string, selector func(compatibility.Target) (compatibility.Selection, error)) (transferPrep, error) {
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return transferPrep{}, fmt.Errorf("prepare FileStation transfer target: %w", err)
	}
	selection, err := selector(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return transferPrep{}, fmt.Errorf("select FileStation transfer backend: %w", err)
	}
	if !selection.Supported {
		return transferPrep{}, fmt.Errorf("FileStation %s is not supported on this NAS", apiName)
	}
	if err := c.loginLocked(ctx); err != nil {
		return transferPrep{}, err
	}
	info, ok := c.target.API(apiName)
	if !ok {
		return transferPrep{}, fmt.Errorf("Synology API %s is not available on this NAS", apiName)
	}
	c.target.AddCapability(capabilityName)
	return transferPrep{
		endpoint:  *c.baseURL,
		apiPath:   info.Path,
		version:   selection.Version,
		sid:       c.sid,
		synoToken: c.synoToken,
		client:    c.streamingClient(),
	}, nil
}

// reestablishForTransfer renews a session that DSM rejected mid-transfer and
// returns the fresh credentials, or a typed session-expired error when it cannot.
func (c *Client) reestablishForTransfer(ctx context.Context) (sid, synoToken string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sid = ""
	c.synoToken = ""
	if reErr := c.reestablishLocked(ctx); reErr != nil {
		if IsSessionExpired(reErr) {
			return "", "", reErr
		}
		return "", "", &SessionExpiredError{Cause: reErr}
	}
	return c.sid, c.synoToken, nil
}

// UploadFile streams src (exactly size bytes, stored as name) into the NAS folder
// dir. When src is an io.Seeker the upload is retried once if DSM rejects the
// session; a non-seekable src cannot be replayed, so a session failure returns a
// typed session-expired error rather than a truncated file.
func (c *Client) UploadFile(ctx context.Context, dir, name string, src io.Reader, size int64, opts UploadOptions) (UploadResult, error) {
	c.mu.Lock()
	prep, err := c.prepareTransferLocked(ctx, filestationops.UploadAPIName, filestationops.UploadCapabilityName, filestationops.SelectUpload)
	c.mu.Unlock()
	if err != nil {
		return UploadResult{}, err
	}
	seeker, seekable := src.(io.Seeker)
	sid, synoToken := prep.sid, prep.synoToken
	for attempt := 0; attempt < 2; attempt++ {
		result, err := doUpload(ctx, prep, sid, synoToken, dir, name, src, size, opts)
		if err == nil {
			return result, nil
		}
		if attempt == 0 && seekable && isSessionError(err) {
			newSID, newToken, reErr := c.reestablishForTransfer(ctx)
			if reErr != nil {
				return UploadResult{}, reErr
			}
			if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr != nil {
				return UploadResult{}, fmt.Errorf("rewind upload source after session renewal: %w", seekErr)
			}
			sid, synoToken = newSID, newToken
			continue
		}
		if isSessionError(err) {
			return UploadResult{}, &SessionExpiredError{Cause: err}
		}
		return UploadResult{}, fmt.Errorf("upload %q to %q: %w", name, dir, err)
	}
	return UploadResult{}, fmt.Errorf("upload %q to %q failed after a session renewal", name, dir)
}

func doUpload(ctx context.Context, prep transferPrep, sid, synoToken, dir, name string, src io.Reader, size int64, opts UploadOptions) (UploadResult, error) {
	boundary, err := randomBoundary()
	if err != nil {
		return UploadResult{}, err
	}
	var head bytes.Buffer
	writer := multipart.NewWriter(&head)
	if err := writer.SetBoundary(boundary); err != nil {
		return UploadResult{}, err
	}
	// DSM requires every parameter field before the file content part.
	fields := [][2]string{
		{"api", filestationops.UploadAPIName},
		{"version", strconv.Itoa(prep.version)},
		{"method", "upload"},
		{"path", dir},
		{"create_parents", strconv.FormatBool(opts.CreateParents)},
		{"overwrite", strconv.FormatBool(opts.Overwrite)},
		{"_sid", sid},
	}
	if synoToken != "" {
		fields = append(fields, [2]string{"SynoToken", synoToken})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return UploadResult{}, err
		}
	}
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	partHeader.Set("Content-Type", "application/octet-stream")
	if _, err := writer.CreatePart(partHeader); err != nil {
		return UploadResult{}, err
	}
	// Close the multipart body by hand so the file bytes stream between the part
	// header and the closing boundary without buffering the file in memory.
	trailer := "\r\n--" + boundary + "--\r\n"
	body := io.MultiReader(bytes.NewReader(head.Bytes()), src, strings.NewReader(trailer))
	contentLength := int64(head.Len()) + size + int64(len(trailer))

	endpoint := prep.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(prep.apiPath, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return UploadResult{}, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "dsmctl/0.1")
	request.ContentLength = contentLength
	if sid != "" {
		request.AddCookie(&http.Cookie{Name: "id", Value: sid})
	}
	if synoToken != "" {
		request.Header.Set("X-SYNO-TOKEN", synoToken)
	}
	response, err := prep.client.Do(request)
	if err != nil {
		return UploadResult{}, fmt.Errorf("request %s: %w", redactTransferURL(endpoint), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return UploadResult{}, fmt.Errorf("request %s returned HTTP %s", redactTransferURL(endpoint), response.Status)
	}
	var envelopeResult envelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&envelopeResult); err != nil {
		return UploadResult{}, fmt.Errorf("decode upload response: %w", err)
	}
	if !envelopeResult.Success {
		code := 0
		if envelopeResult.Error != nil {
			code = envelopeResult.Error.Code
		}
		return UploadResult{}, &APIError{API: filestationops.UploadAPIName, Method: "upload", Code: code}
	}
	return decodeUploadResult(envelopeResult.Data), nil
}

func decodeUploadResult(data json.RawMessage) UploadResult {
	var raw struct {
		File   *string `json:"file"`
		BlSkip *bool   `json:"blSkip"`
	}
	_ = json.Unmarshal(data, &raw)
	result := UploadResult{}
	if raw.File != nil {
		result.File = strings.TrimSpace(*raw.File)
	}
	result.Skipped = raw.BlSkip != nil && *raw.BlSkip
	return result
}

// DownloadFile opens a streaming read of a single NAS path. The client mutex is
// released before the live body is returned, so the caller streams without
// blocking other client calls. The caller closes the returned Body.
func (c *Client) DownloadFile(ctx context.Context, path string) (*DownloadContent, error) {
	c.mu.Lock()
	prep, err := c.prepareTransferLocked(ctx, filestationops.DownloadAPIName, filestationops.DownloadCapabilityName, filestationops.SelectDownload)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sid, synoToken := prep.sid, prep.synoToken
	for attempt := 0; attempt < 2; attempt++ {
		content, err := doDownload(ctx, prep, sid, synoToken, path)
		if err == nil {
			return content, nil
		}
		if attempt == 0 && isSessionError(err) {
			// Nothing has been streamed to the caller yet, so renewing and
			// retrying is safe.
			newSID, newToken, reErr := c.reestablishForTransfer(ctx)
			if reErr != nil {
				return nil, reErr
			}
			sid, synoToken = newSID, newToken
			continue
		}
		if isSessionError(err) {
			return nil, &SessionExpiredError{Cause: err}
		}
		return nil, fmt.Errorf("download %q: %w", path, err)
	}
	return nil, fmt.Errorf("download %q failed after a session renewal", path)
}

func doDownload(ctx context.Context, prep transferPrep, sid, synoToken, path string) (*DownloadContent, error) {
	endpoint := prep.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(prep.apiPath, "/")
	query := url.Values{
		"api":     {filestationops.DownloadAPIName},
		"version": {strconv.Itoa(prep.version)},
		"method":  {"download"},
		"path":    {path},
		"mode":    {"download"},
	}
	if sid != "" {
		query.Set("_sid", sid)
	}
	if synoToken != "" {
		query.Set("SynoToken", synoToken)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "dsmctl/0.1")
	if sid != "" {
		request.AddCookie(&http.Cookie{Name: "id", Value: sid})
	}
	if synoToken != "" {
		request.Header.Set("X-SYNO-TOKEN", synoToken)
	}
	response, err := prep.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", redactTransferURL(endpoint), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, fmt.Errorf("request %s returned HTTP %s", redactTransferURL(endpoint), response.Status)
	}
	// DSM streams the file bytes on success but returns a JSON envelope on error
	// (auth failure, path not found). Branch on the content type before handing
	// the body to the caller.
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		defer response.Body.Close()
		var envelopeResult envelope
		if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&envelopeResult); err != nil {
			return nil, fmt.Errorf("decode download error response: %w", err)
		}
		if !envelopeResult.Success {
			code := 0
			if envelopeResult.Error != nil {
				code = envelopeResult.Error.Code
			}
			return nil, &APIError{API: filestationops.DownloadAPIName, Method: "download", Code: code}
		}
		return nil, fmt.Errorf("download returned an unexpected JSON response")
	}
	content := &DownloadContent{
		Body:        response.Body,
		Size:        response.ContentLength,
		ContentType: contentType,
		Filename:    filenameFromDisposition(response.Header.Get("Content-Disposition")),
	}
	return content, nil
}

// ThumbnailOptions selects which rendition of an image thumbnail to fetch.
type ThumbnailOptions struct {
	// Size is one of small, medium, large, or original; empty defaults to small.
	Size string
	// Rotate is the DSM rotation index 0-4 (0 = none).
	Rotate int
}

// FileStationThumbnail opens a streaming read of an image thumbnail. Like
// DownloadFile it releases the client mutex before returning the live body,
// which the caller closes. A non-image path or a thumbnail DSM cannot render
// returns a typed API error rather than a body.
func (c *Client) FileStationThumbnail(ctx context.Context, path string, opts ThumbnailOptions) (*DownloadContent, error) {
	c.mu.Lock()
	prep, err := c.prepareTransferLocked(ctx, filestationops.ThumbAPIName, filestationops.ThumbCapabilityName, filestationops.SelectThumb)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sid, synoToken := prep.sid, prep.synoToken
	for attempt := 0; attempt < 2; attempt++ {
		content, err := doThumbnail(ctx, prep, sid, synoToken, path, opts)
		if err == nil {
			return content, nil
		}
		if attempt == 0 && isSessionError(err) {
			newSID, newToken, reErr := c.reestablishForTransfer(ctx)
			if reErr != nil {
				return nil, reErr
			}
			sid, synoToken = newSID, newToken
			continue
		}
		if isSessionError(err) {
			return nil, &SessionExpiredError{Cause: err}
		}
		return nil, fmt.Errorf("thumbnail %q: %w", path, err)
	}
	return nil, fmt.Errorf("thumbnail %q failed after a session renewal", path)
}

func doThumbnail(ctx context.Context, prep transferPrep, sid, synoToken, path string, opts ThumbnailOptions) (*DownloadContent, error) {
	size := opts.Size
	if size == "" {
		size = "small"
	}
	endpoint := prep.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(prep.apiPath, "/")
	query := url.Values{
		"api":     {filestationops.ThumbAPIName},
		"version": {strconv.Itoa(prep.version)},
		"method":  {"get"},
		"path":    {path},
		"size":    {size},
		"rotate":  {strconv.Itoa(opts.Rotate)},
	}
	if sid != "" {
		query.Set("_sid", sid)
	}
	if synoToken != "" {
		query.Set("SynoToken", synoToken)
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "dsmctl/0.1")
	if sid != "" {
		request.AddCookie(&http.Cookie{Name: "id", Value: sid})
	}
	if synoToken != "" {
		request.Header.Set("X-SYNO-TOKEN", synoToken)
	}
	response, err := prep.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", redactTransferURL(endpoint), err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, fmt.Errorf("request %s returned HTTP %s", redactTransferURL(endpoint), response.Status)
	}
	// DSM streams the image bytes on success but returns a JSON envelope on
	// error (auth failure, not an image, thumbnail unavailable). Branch on the
	// content type before handing the body to the caller.
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		defer response.Body.Close()
		var envelopeResult envelope
		if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&envelopeResult); err != nil {
			return nil, fmt.Errorf("decode thumbnail error response: %w", err)
		}
		if !envelopeResult.Success {
			code := 0
			if envelopeResult.Error != nil {
				code = envelopeResult.Error.Code
			}
			return nil, &APIError{API: filestationops.ThumbAPIName, Method: "get", Code: code}
		}
		return nil, fmt.Errorf("thumbnail returned an unexpected JSON response")
	}
	return &DownloadContent{
		Body:        response.Body,
		Size:        response.ContentLength,
		ContentType: contentType,
		Filename:    filenameFromDisposition(response.Header.Get("Content-Disposition")),
	}, nil
}

// redactTransferURL returns endpoint with credential query parameters masked,
// safe to embed in an error or log. url.URL.Redacted only masks userinfo, not
// the _sid/SynoToken query parameters the download and thumbnail transports set,
// so the raw endpoint would otherwise leak the session id and token — which the
// secrets contract forbids from any error or log.
func redactTransferURL(endpoint url.URL) string {
	query := endpoint.Query()
	redacted := false
	for _, key := range []string{"_sid", "SynoToken"} {
		if query.Has(key) {
			query.Set(key, "REDACTED")
			redacted = true
		}
	}
	if redacted {
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.Redacted()
}

func filenameFromDisposition(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["filename"])
}

func randomBoundary() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate multipart boundary: %w", err)
	}
	return "dsmctl" + hex.EncodeToString(buffer), nil
}
