package weblogin

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Enrollment is a server-side PKCE transaction for a browser hosted by the
// gateway administration page. Verifier and expected state never leave the
// gateway process.
type Enrollment struct {
	base        string
	clientID    string
	sessionName string
	verifier    string
	state       string
	httpClient  *http.Client
}

type EnrollmentStart struct {
	LoginURL string
	State    string
}

// BeginEnrollment creates the NAS sign-in URL used by a gateway admin page.
// openerURL must be the authenticated gateway page that opens the DSM popup.
func BeginEnrollment(baseURL, openerURL string, opts Options) (*Enrollment, EnrollmentStart, error) {
	base, _, err := normalizeBase(baseURL)
	if err != nil {
		return nil, EnrollmentStart{}, err
	}
	opener, err := url.Parse(strings.TrimSpace(openerURL))
	if err != nil || opener.Host == "" || (opener.Scheme != "http" && opener.Scheme != "https") {
		return nil, EnrollmentStart{}, errors.New("opener URL must be an absolute http or https URL")
	}
	verifier, challenge, err := newPKCE()
	if err != nil {
		return nil, EnrollmentStart{}, err
	}
	state, err := randomToken(16)
	if err != nil {
		return nil, EnrollmentStart{}, err
	}
	clientID := firstNonEmpty(opts.ClientID, defaultClientID)
	sessionName := firstNonEmpty(opts.SessionName, defaultSession)
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	exchangeBase := base
	if strings.TrimSpace(opts.ExchangeBaseURL) != "" {
		exchangeBase, _, err = normalizeBase(opts.ExchangeBaseURL)
		if err != nil {
			return nil, EnrollmentStart{}, fmt.Errorf("exchange base URL: %w", err)
		}
	}
	loginURL := base + "/?" + url.Values{
		"forceDesktop":          {"1"},
		"client_id":             {clientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"opener":                {opener.String()},
		"state":                 {state},
		"session":               {sessionName},
		"force_login":           {"yes"},
	}.Encode() + "#/signin"
	return &Enrollment{base: exchangeBase, clientID: clientID, sessionName: sessionName, verifier: verifier, state: state, httpClient: httpClient}, EnrollmentStart{LoginURL: loginURL, State: state}, nil
}

// Complete exchanges the one-time code at most once at the caller's layer.
// It validates OAuth state before any DSM request is sent.
func (e *Enrollment) Complete(ctx context.Context, code, rs, state string) (Result, error) {
	if e == nil || strings.TrimSpace(code) == "" || strings.TrimSpace(rs) == "" {
		return Result{}, errors.New("web-login code and server key are required")
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(e.state)) != 1 {
		return Result{}, errors.New("web-login state does not match")
	}
	return exchange(ctx, e.base, e.clientID, e.sessionName, code, e.verifier, rs, e.httpClient)
}
