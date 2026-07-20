package synology

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/certificate"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	certops "github.com/ychiu1211/dsmctl/internal/synology/operations/certificate"
)

// CertificateImportRequest carries the bytes and metadata for a certificate
// import. Key holds the private-key PEM; the application layer resolves it from a
// credential reference only at apply time and zeroizes it right after this call
// returns. It is a value on this request struct and never persisted, logged, or
// echoed back.
type CertificateImportRequest struct {
	Key          []byte // private-key PEM — secret; never logged or returned
	Leaf         []byte // leaf certificate PEM — public
	Intermediate []byte // optional intermediate chain PEM — public
	ReplaceID    string // empty = new install; set = replace
	Description  string
	AsDefault    bool
}

// CertificateMutationResult is the normalized write outcome (no key material).
type CertificateMutationResult = certificate.MutationResult

// ImportCertificate uploads a certificate bundle via a multipart POST that
// mirrors the FileStation streaming upload transport (WI-049): the private key,
// leaf, and optional intermediate ride as file parts of the request body ONLY.
// The key never appears in a URL, query, header, log line, or the returned
// result. On success DSM returns the new/updated certificate id.
//
// LIVE-VERIFIED (WI-065, DSM 7.3): the multipart part names (key/cert/inter_cert),
// the `import` method, the entry.cgi endpoint, and the id/desc/_sid form fields
// are confirmed by a successful live import — with the api set to the PARENT
// SYNO.Core.Certificate (CRTImportAPIName), NOT SYNO.Core.Certificate.CRT (which
// rejects import with code 103). The one still-unverified detail is `as_default`;
// see doCertificateImport.
func (c *Client) ImportCertificate(ctx context.Context, request CertificateImportRequest) (CertificateMutationResult, error) {
	c.mu.Lock()
	prep, err := c.prepareCertificateImportLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return CertificateMutationResult{}, err
	}
	// The private-key bytes cannot be replayed (they are zeroized by the caller
	// right after), so a rejected session is reported rather than retried.
	result, err := doCertificateImport(ctx, prep, request)
	if err != nil {
		if isSessionError(err) {
			return CertificateMutationResult{}, &SessionExpiredError{Cause: err}
		}
		return CertificateMutationResult{}, err
	}
	return result, nil
}

// doCertificateImport builds and sends the multipart import. The private key
// rides as a file PART of the request body and NOWHERE else — not the URL, not a
// query parameter, not a header. A key PEM is small, so the body is buffered
// rather than streamed.
func doCertificateImport(ctx context.Context, prep transferPrep, request CertificateImportRequest) (CertificateMutationResult, error) {
	boundary, err := randomBoundary()
	if err != nil {
		return CertificateMutationResult{}, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.SetBoundary(boundary); err != nil {
		return CertificateMutationResult{}, err
	}
	// LIVE-VERIFIED (DSM 7.3): import is a method on the PARENT api
	// SYNO.Core.Certificate, not on CRT (CRT/import is code 103).
	fields := [][2]string{
		{"api", certops.CRTImportAPIName},
		{"version", strconv.Itoa(prep.version)},
		{"method", certops.CRTImportMethod},
		{certops.ImportFieldID, request.ReplaceID},
		{certops.ImportFieldDesc, request.Description},
		{"_sid", prep.sid},
	}
	// WIRE-UNVERIFIED (as_default): re-confirm live. DSM parsed the multipart
	// `as_default` form field as truthy for ANY non-empty value, so sending
	// "false" marked the newly-imported cert default despite the caller asking
	// not to. Send the part ONLY when the caller wants this cert to become the
	// default; omitting it entirely leaves the existing default cert in place.
	if request.AsDefault {
		fields = append(fields, [2]string{certops.ImportFieldAsDefault, "true"})
	}
	if prep.synoToken != "" {
		fields = append(fields, [2]string{"SynoToken", prep.synoToken})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return CertificateMutationResult{}, err
		}
	}
	// File parts: the private key rides here and NOWHERE else.
	if err := writeFilePart(writer, certops.ImportFieldKey, "server.key", request.Key); err != nil {
		return CertificateMutationResult{}, err
	}
	if err := writeFilePart(writer, certops.ImportFieldCert, "server.crt", request.Leaf); err != nil {
		return CertificateMutationResult{}, err
	}
	if len(request.Intermediate) > 0 {
		if err := writeFilePart(writer, certops.ImportFieldInterCert, "chain.crt", request.Intermediate); err != nil {
			return CertificateMutationResult{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return CertificateMutationResult{}, err
	}

	endpoint := prep.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(prep.apiPath, "/")
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body.Bytes()))
	if err != nil {
		return CertificateMutationResult{}, err
	}
	httpRequest.Header.Set("Content-Type", writer.FormDataContentType())
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "dsmctl/0.1")
	if prep.sid != "" {
		httpRequest.AddCookie(&http.Cookie{Name: "id", Value: prep.sid})
	}
	if prep.synoToken != "" {
		httpRequest.Header.Set("X-SYNO-TOKEN", prep.synoToken)
	}

	response, err := prep.client.Do(httpRequest)
	if err != nil {
		return CertificateMutationResult{}, redactTransferError(endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return CertificateMutationResult{}, fmt.Errorf("request %s returned HTTP %s", redactTransferURL(endpoint), response.Status)
	}
	var envelopeResult envelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&envelopeResult); err != nil {
		return CertificateMutationResult{}, fmt.Errorf("decode certificate import response: %w", err)
	}
	if !envelopeResult.Success {
		code := 0
		if envelopeResult.Error != nil {
			code = envelopeResult.Error.Code
		}
		return CertificateMutationResult{}, &APIError{API: certops.CRTImportAPIName, Method: certops.CRTImportMethod, Code: code}
	}
	result := CertificateMutationResult{Action: certificate.ActionImport, Description: request.Description, AsDefault: request.AsDefault}
	result.CertID = decodeImportedCertID(envelopeResult.Data, request.ReplaceID)
	return result, nil
}

func writeFilePart(writer *multipart.Writer, field, filename string, data []byte) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func decodeImportedCertID(data json.RawMessage, fallback string) string {
	var raw struct {
		ID   string `json:"id"`
		Cert struct {
			ID string `json:"id"`
		} `json:"certificate"`
	}
	if json.Unmarshal(data, &raw) == nil {
		if raw.ID != "" {
			return raw.ID
		}
		if raw.Cert.ID != "" {
			return raw.Cert.ID
		}
	}
	return fallback
}

// SetDefaultCertificate makes an installed certificate the default via CRT set.
func (c *Client) SetDefaultCertificate(ctx context.Context, id, description string) (CertificateMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, certops.MutationAPINames()...); err != nil {
		return CertificateMutationResult{}, fmt.Errorf("prepare certificate set-default target: %w", err)
	}
	if !certops.SupportsCRTWrites(c.target) {
		return CertificateMutationResult{}, fmt.Errorf("%s is not supported on this NAS", certops.CRTAPIName)
	}
	params := map[string]any{
		certops.SetFieldID:        id,
		certops.SetFieldAsDefault: true,
	}
	if description != "" {
		params[certops.SetFieldDesc] = description
	}
	_, err := lockedExecutor{client: c}.Execute(ctx, compatibilityRequest(certops.CRTAPIName, certops.CRTSetMethod, params))
	if err != nil {
		return CertificateMutationResult{}, fmt.Errorf("set default certificate: %w", err)
	}
	c.target.AddCapability(certops.SetDefaultCapabilityName)
	return CertificateMutationResult{Action: certificate.ActionSetDefault, CertID: id, AsDefault: true}, nil
}

// BindCertificateService binds a DSM service key to a certificate via Service set.
func (c *Client) BindCertificateService(ctx context.Context, service, certID string) (CertificateMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, certops.MutationAPINames()...); err != nil {
		return CertificateMutationResult{}, fmt.Errorf("prepare certificate bind target: %w", err)
	}
	if !certops.SupportsServiceBinding(c.target) {
		return CertificateMutationResult{}, fmt.Errorf("%s is not supported on this NAS", certops.ServiceAPIName)
	}
	// WIRE-UNVERIFIED (WI-065): Service.set takes a JSON `settings` array of
	// {service, id} bindings.
	settings := []map[string]any{{"service": service, "id": certID}}
	params := map[string]any{certops.ServiceSetFieldSettings: settings}
	_, err := lockedExecutor{client: c}.Execute(ctx, compatibilityRequest(certops.ServiceAPIName, certops.ServiceSetMethod, params))
	if err != nil {
		return CertificateMutationResult{}, fmt.Errorf("bind service %q to certificate: %w", service, err)
	}
	c.target.AddCapability(certops.BindServiceCapabilityName)
	return CertificateMutationResult{Action: certificate.ActionBindService, CertID: certID}, nil
}

// DeleteCertificate removes an installed certificate via CRT delete.
func (c *Client) DeleteCertificate(ctx context.Context, id string) (CertificateMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, certops.MutationAPINames()...); err != nil {
		return CertificateMutationResult{}, fmt.Errorf("prepare certificate delete target: %w", err)
	}
	if !certops.SupportsCRTWrites(c.target) {
		return CertificateMutationResult{}, fmt.Errorf("%s is not supported on this NAS", certops.CRTAPIName)
	}
	// WIRE-UNVERIFIED (delete id-vs-ids): re-confirm live. The shipped
	// `{"id":[...]}` array reached the DSM delete handler and returned a domain
	// error (not a missing-arg error), so the `id` array param is likely correct.
	params := map[string]any{certops.DeleteFieldID: []string{id}}
	_, err := lockedExecutor{client: c}.Execute(ctx, compatibilityRequest(certops.CRTAPIName, certops.CRTDeleteMethod, params))
	if err != nil {
		return CertificateMutationResult{}, fmt.Errorf("delete certificate: %w", err)
	}
	c.target.AddCapability(certops.DeleteCapabilityName)
	return CertificateMutationResult{Action: certificate.ActionDelete, CertID: id}, nil
}

// compatibilityRequest builds a mutating (never auto-retried) JSON-parameter
// request. ReadOnly is left false so a write is issued exactly once.
func compatibilityRequest(api, method string, params map[string]any) compatibility.Request {
	return compatibility.Request{API: api, Version: 1, Method: method, JSONParameters: params}
}

// prepareCertificateImportLocked discovers the CRT API, ensures a session, and
// captures the immutable inputs for the multipart import — mirroring
// prepareTransferLocked but for the certificate CRT family. It uses the
// streaming HTTP client so a large bundle is bounded only by the caller's
// context. It runs entirely under c.mu; the caller unlocks before streaming.
func (c *Client) prepareCertificateImportLocked(ctx context.Context) (transferPrep, error) {
	if err := c.prepareCompatibilityTargetLocked(ctx, certops.MutationAPINames()...); err != nil {
		return transferPrep{}, fmt.Errorf("prepare certificate import target: %w", err)
	}
	if !certops.SupportsCRTWrites(c.target) {
		return transferPrep{}, fmt.Errorf("%s is not supported on this NAS", certops.CRTAPIName)
	}
	if err := c.loginLocked(ctx); err != nil {
		return transferPrep{}, err
	}
	info, ok := c.target.API(certops.CRTAPIName)
	if !ok {
		return transferPrep{}, fmt.Errorf("Synology API %s is not available on this NAS", certops.CRTAPIName)
	}
	c.target.AddCapability(certops.ImportCapabilityName)
	return transferPrep{
		endpoint:  *c.baseURL,
		apiPath:   info.Path,
		version:   1,
		sid:       c.sid,
		synoToken: c.synoToken,
		client:    c.streamingClient(),
	}, nil
}

// ExportCertificate opens a streaming read of the archive DSM produces for a
// certificate. The archive CONTAINS the private-key PEM, so this is modeled like
// a FileStation download: the caller writes the live body to a local file and
// closes it; no key bytes are ever returned over MCP. Transfer errors redact the
// _sid/SynoToken query parameters (the redactTransferURL lesson from WI-049).
//
// WIRE-UNVERIFIED (WI-065): the CRT `export` method and its `id` parameter are
// the spec author's best knowledge; confirm with a throwaway DSMCTL_DUMP probe.
func (c *Client) ExportCertificate(ctx context.Context, id string) (*DownloadContent, error) {
	c.mu.Lock()
	prep, err := c.prepareCertificateImportLocked(ctx)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sid, synoToken := prep.sid, prep.synoToken
	for attempt := 0; attempt < 2; attempt++ {
		content, err := doCertificateExport(ctx, prep, sid, synoToken, id)
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
		return nil, fmt.Errorf("export certificate %q: %w", id, err)
	}
	return nil, fmt.Errorf("export certificate %q failed after a session renewal", id)
}

func doCertificateExport(ctx context.Context, prep transferPrep, sid, synoToken, id string) (*DownloadContent, error) {
	endpoint := prep.endpoint
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(prep.apiPath, "/")
	query := endpoint.Query()
	query.Set("api", certops.CRTAPIName)
	query.Set("version", strconv.Itoa(prep.version))
	query.Set("method", certops.CRTExportMethod)
	query.Set(certops.ExportFieldID, id)
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
		return nil, redactTransferError(endpoint, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		return nil, fmt.Errorf("request %s returned HTTP %s", redactTransferURL(endpoint), response.Status)
	}
	contentType := response.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		defer response.Body.Close()
		var envelopeResult envelope
		if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&envelopeResult); err != nil {
			return nil, fmt.Errorf("decode certificate export error response: %w", err)
		}
		if !envelopeResult.Success {
			code := 0
			if envelopeResult.Error != nil {
				code = envelopeResult.Error.Code
			}
			return nil, &APIError{API: certops.CRTAPIName, Method: certops.CRTExportMethod, Code: code}
		}
		return nil, fmt.Errorf("certificate export returned an unexpected JSON response")
	}
	return &DownloadContent{
		Body:        response.Body,
		Size:        response.ContentLength,
		ContentType: contentType,
		Filename:    filenameFromDisposition(response.Header.Get("Content-Disposition")),
	}, nil
}

// RepinLeafFingerprint updates the client's pinned-TLS expectation to a new leaf
// fingerprint. It is used by the current-session protection: after importing a
// replacement for the DSM-serving certificate, the leaf dsmctl is pinned to
// changes, so the post-apply re-read must expect the NEW fingerprint (known
// locally from the imported PEM) rather than treat the pinning break as a
// failure. When the client is not in pinned mode this is a no-op.
func (c *Client) RepinLeafFingerprint(fingerprintHex string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.VerifyConnection == nil {
		// Not a pinned client (standard chain validation or insecure). The new
		// leaf will be accepted by the normal path; nothing to re-pin.
		return nil
	}
	newConfig, err := repinTLSConfig(transport.TLSClientConfig, fingerprintHex)
	if err != nil {
		return err
	}
	transport.TLSClientConfig = newConfig
	// Existing pooled connections were authenticated against the old leaf; force
	// the re-read to dial fresh so it verifies against the new pin.
	transport.CloseIdleConnections()
	return nil
}

// repinTLSConfig returns a clone of cfg whose VerifyConnection pins the given
// SHA-256 leaf fingerprint. It is a pure helper so the re-pin logic is unit
// tested without a live TLS server.
func repinTLSConfig(cfg *tls.Config, fingerprintHex string) (*tls.Config, error) {
	expected, err := hex.DecodeString(strings.ReplaceAll(fingerprintHex, ":", ""))
	if err != nil || len(expected) != sha256.Size {
		return nil, fmt.Errorf("re-pin requires a valid SHA-256 leaf fingerprint")
	}
	clone := cfg.Clone()
	clone.InsecureSkipVerify = true //nolint:gosec // explicit pin replaces chain validation
	clone.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return fmt.Errorf("TLS peer did not provide a certificate")
		}
		actual := sha256.Sum256(state.PeerCertificates[0].Raw)
		if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
			return fmt.Errorf("TLS server certificate does not match the re-pinned SHA-256 fingerprint")
		}
		return nil
	}
	return clone, nil
}
