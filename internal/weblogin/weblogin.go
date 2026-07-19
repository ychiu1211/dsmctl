// Package weblogin performs the DSM browser web-login (SYNO.API.Auth "webui"
// OAuth2 authorization-code + PKCE flow) from a command-line tool.
//
// The account password is never handled here: the user authenticates in their
// own browser against the target DSM's own sign-in page (password, 2FA,
// passkey, approve sign-in are all DSM's concern). dsmctl only receives the
// short-lived one-time code the DSM hands back and exchanges it, over a
// Noise_IK secure channel, for a normal DSM session (SID + SynoToken) plus the
// durable Noise key material that lets the session be resumed later without a
// browser.
//
// The code is delivered to a loopback HTTP server via the DSM sign-in page's
// window.opener postMessage (RFC 8252-style native-app login, adapted to the
// DSM "opener" mechanism rather than a plain redirect, which the DSM restricts
// to same-origin).
package weblogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/flynn/noise"

	"github.com/ychiu1211/dsmctl/internal/webassets"
)

const (
	defaultClientID = "webui"
	// defaultSession is an app session name, deliberately not "webui". DSM keeps
	// an app-named session resumable after its idle timeout (it is suspended, not
	// destroyed), whereas a "webui" session is dropped non-resumably and forces a
	// fresh browser sign-in. Using an app name lets a web login survive idle
	// periods through the browserless Noise resume, the way the Synology mobile
	// apps stay signed in. Verified live: the DSM code-grant browser flow accepts
	// this session name.
	defaultSession = "dsmctl"
	defaultTimeout = 3 * time.Minute
	maxBodySize    = 1 << 20
)

// Result is the outcome of a successful web login. SID and SynoToken are the
// live DSM session; ServerPublicKey, LocalPublicKey and LocalPrivateKey are the
// durable Noise resume material (persist them to refresh the session later
// without a browser). All fields are authentication material.
type Result struct {
	Account         string
	SID             string
	SynoToken       string
	DeviceID        string
	ServerPublicKey []byte
	LocalPublicKey  []byte
	LocalPrivateKey []byte
}

// Options tunes the login flow. The zero value is usable; only HTTPClient is
// normally worth setting (to match a profile's TLS policy).
type Options struct {
	ClientID    string
	SessionName string
	HTTPClient  *http.Client
	// OpenBrowser launches the user's browser at the given loopback URL. When
	// nil the OS default handler is used. It may return an error (for example
	// on a headless host); the URL is always printed so the user can open it
	// manually.
	OpenBrowser func(url string) error
	// Prompt receives human-facing progress lines (the URL to open, status).
	Prompt  io.Writer
	Timeout time.Duration
}

// Login runs the interactive web-login flow against a DSM base URL such as
// "https://nas.example.com:5001" and returns the resulting session. It blocks
// until the user finishes signing in, the context is cancelled, or the timeout
// elapses.
func Login(ctx context.Context, baseURL string, opts Options) (Result, error) {
	base, origin, err := normalizeBase(baseURL)
	if err != nil {
		return Result{}, err
	}
	clientID := firstNonEmpty(opts.ClientID, defaultClientID)
	sessionName := firstNonEmpty(opts.SessionName, defaultSession)
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	prompt := opts.Prompt
	if prompt == nil {
		prompt = io.Discard
	}
	open := opts.OpenBrowser
	if open == nil {
		open = openBrowser
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	verifier, challenge, err := newPKCE()
	if err != nil {
		return Result{}, err
	}
	state, err := randomToken(16)
	if err != nil {
		return Result{}, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()
	loopback := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)

	loginURL := base + "/?" + url.Values{
		"forceDesktop":          {"1"},
		"client_id":             {clientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"opener":                {loopback},
		"state":                 {state},
		"session":               {sessionName},
		"force_login":           {"yes"},
	}.Encode() + "#/signin"

	page := buildPage(loginURL, origin)

	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)

	callback := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Code  string `json:"code"`
			Rs    string `json:"rs"`
			State string `json:"state"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBodySize)).Decode(&payload); err != nil || payload.Code == "" || payload.Rs == "" {
			http.Error(w, "invalid callback payload", http.StatusBadRequest)
			return
		}
		res, err := exchange(ctx, base, clientID, sessionName, payload.Code, verifier, payload.Rs, httpClient)
		if err != nil {
			http.Error(w, "sign-in failed; return to the terminal", http.StatusBadGateway)
			select {
			case errCh <- err:
			default:
			}
			return
		}
		_, _ = io.WriteString(w, "Signed in. You can close this window and return to the terminal.")
		select {
		case resultCh <- res:
		default:
		}
	}

	server := &http.Server{Handler: newLoopbackHandler(page, callback)}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(prompt, "Opening your browser to sign in to %s\n", base)
	fmt.Fprintf(prompt, "If it does not open, browse to: %s\n", loopback)
	if err := open(loopback); err != nil {
		fmt.Fprintf(prompt, "(could not launch a browser automatically: %v)\n", err)
	}

	select {
	case res := <-resultCh:
		return res, nil
	case err := <-errCh:
		return Result{}, err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{}, errors.New("web login timed out before sign-in completed")
		}
		return Result{}, ctx.Err()
	}
}

func newLoopbackHandler(page string, callback http.HandlerFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	})
	mux.HandleFunc("/favicon.svg", webassets.ServeFavicon)
	mux.HandleFunc("/callback", callback)
	return mux
}

// exchange trades the one-time code for a session over a Noise_IK handshake,
// matching the DSM "webui" v7 code-grant login.
func exchange(ctx context.Context, base, clientID, sessionName, code, verifier, rs string, httpClient *http.Client) (Result, error) {
	suite := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2b)
	keypair, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		return Result{}, fmt.Errorf("generate noise keypair: %w", err)
	}
	serverStatic, err := decodeB64URL(rs)
	if err != nil {
		return Result{}, fmt.Errorf("decode server key: %w", err)
	}
	handshake, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   suite,
		Random:        rand.Reader,
		Pattern:       noise.HandshakeIK,
		Initiator:     true,
		StaticKeypair: keypair,
		PeerStatic:    serverStatic,
	})
	if err != nil {
		return Result{}, fmt.Errorf("init noise handshake: %w", err)
	}
	ikMessage, _, _, err := handshake.WriteMessage(nil, nil)
	if err != nil {
		return Result{}, fmt.Errorf("build noise message: %w", err)
	}

	form := url.Values{
		"api":               {"SYNO.API.Auth"},
		"method":            {"login"},
		"version":           {"7"},
		"type":              {"code"},
		"client_id":         {clientID},
		"session":           {sessionName},
		"code":              {code},
		"code_verifier":     {verifier},
		"ik_message":        {base64.URLEncoding.EncodeToString(ikMessage)},
		"enable_syno_token": {"yes"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/webapi/entry.cgi", strings.NewReader(form.Encode()))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("exchange code: %w", err)
	}
	defer response.Body.Close()

	var decoded struct {
		Success bool `json:"success"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
		Data struct {
			Account   string `json:"account"`
			SID       string `json:"sid"`
			SynoToken string `json:"synotoken"`
			DeviceID  string `json:"device_id"`
			IKMessage string `json:"ik_message"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxBodySize)).Decode(&decoded); err != nil {
		return Result{}, fmt.Errorf("decode exchange response: %w", err)
	}
	if !decoded.Success || decoded.Data.SID == "" {
		code := 0
		if decoded.Error != nil {
			code = decoded.Error.Code
		}
		return Result{}, fmt.Errorf("DSM rejected the web-login code exchange (error %d)", code)
	}
	// Completing the handshake authenticates the server's static key.
	if decoded.Data.IKMessage != "" {
		if serverMessage, err := decodeB64URL(decoded.Data.IKMessage); err == nil {
			_, _, _, _ = handshake.ReadMessage(nil, serverMessage)
		}
	}
	return Result{
		Account:         decoded.Data.Account,
		SID:             decoded.Data.SID,
		SynoToken:       decoded.Data.SynoToken,
		DeviceID:        decoded.Data.DeviceID,
		ServerPublicKey: serverStatic,
		LocalPublicKey:  keypair.Public,
		LocalPrivateKey: keypair.Private,
	}, nil
}

func newPKCE() (verifier, challenge string, err error) {
	verifier, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func decodeB64URL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func normalizeBase(raw string) (base, origin string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", "", errors.New("base URL must be an absolute http or https URL")
	}
	origin = parsed.Scheme + "://" + parsed.Host
	return origin, origin, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func openBrowser(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
