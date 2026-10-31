package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const verifyEndpoint = "https://otxfylhjrlaesjagfhfi.supabase.co/functions/v1/invoke-verify-check"
const jaipilotSite = "https://www.jaipilot.com"

type managedReply struct {
	Proposal    checkProposal `json:"proposal"`
	CreditsUsed int           `json:"creditsUsed"`
	Pending     bool          `json:"pending"`
	Error       string        `json:"error"`
}
type proposalError struct{ reason string }

func (e proposalError) Error() string { return e.reason }

type managedSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Email        string `json:"email"`
}

func managedProposal(ctx context.Context, input checkRequest) (checkProposal, int, error) {
	token, err := managedToken(ctx)
	if err != nil {
		return checkProposal{}, 0, err
	}
	endpoint := os.Getenv("JAIPILOT_VERIFY_ENDPOINT")
	if endpoint == "" {
		endpoint = verifyEndpoint
	}
	body, err := json.Marshal(input)
	if err != nil {
		return checkProposal{}, 0, err
	}
	client := &http.Client{Timeout: 125 * time.Second}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if err != nil {
			return checkProposal{}, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return checkProposal{}, 0, ctx.Err()
			}
			if e := waitRetry(ctx); e != nil {
				return checkProposal{}, 0, e
			}
			continue
		}
		var reply managedReply
		readErr := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&reply)
		response.Body.Close()
		if readErr != nil {
			if e := waitRetry(ctx); e != nil {
				return checkProposal{}, 0, e
			}
			continue
		}
		if response.StatusCode == 202 || reply.Pending {
			if e := waitRetry(ctx); e != nil {
				return checkProposal{}, 0, e
			}
			continue
		}
		if response.StatusCode != 200 {
			if reply.Error != "" {
				if response.StatusCode == 422 {
					return checkProposal{}, reply.CreditsUsed, proposalError{reply.Error}
				}
				return checkProposal{}, reply.CreditsUsed, errors.New(reply.Error)
			}
			return checkProposal{}, 0, fmt.Errorf("JAIPilot Verify returned HTTP %d", response.StatusCode)
		}
		return reply.Proposal, reply.CreditsUsed, nil
	}
	return checkProposal{}, 0, errors.New("verification did not complete within three minutes; the run may still be processing")
}

func waitRetry(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func managedSessionPath() string {
	base := os.Getenv("JAIPILOT_CONFIG_DIR")
	if base == "" {
		if runtime.GOOS == "windows" {
			base = os.Getenv("APPDATA")
		} else {
			base = os.Getenv("XDG_CONFIG_HOME")
		}
	}
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "jaipilot", "session.json")
}
func managedToken(ctx context.Context) (string, error) {
	if token := os.Getenv("JAIPILOT_VERIFY_TOKEN"); token != "" {
		return token, nil
	}
	path := managedSessionPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return managedLogin(ctx)
	}
	if err != nil {
		return "", errors.New("JAIPilot session could not be read")
	}
	var session managedSession
	if json.Unmarshal(data, &session) != nil || session.AccessToken == "" || session.RefreshToken == "" {
		return managedLogin(ctx)
	}
	if session.ExpiresAt > time.Now().Unix()+60 {
		return session.AccessToken, nil
	}
	refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(refreshCtx, "POST", jaipilotSite+"/plugin-refresh", strings.NewReader(jsonText(object{"refresh_token": session.RefreshToken})))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("could not refresh JAIPilot sign-in")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return managedLogin(ctx)
	}
	if response.StatusCode != 200 {
		return "", fmt.Errorf("could not refresh JAIPilot sign-in (HTTP %d)", response.StatusCode)
	}
	var updated managedSession
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&updated); err != nil || updated.AccessToken == "" {
		return "", errors.New("JAIPilot returned an invalid session")
	}
	if updated.RefreshToken == "" {
		updated.RefreshToken = session.RefreshToken
	}
	if updated.Email == "" {
		updated.Email = session.Email
	}
	if err = atomicJSON(path, updated, 0600); err != nil {
		return "", err
	}
	return updated.AccessToken, nil
}

func managedLogin(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	state := uuid()
	completed := make(chan managedSession, 1)
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/auth/callback" || q.Get("state") != state {
			http.Error(w, "Invalid callback", 400)
			return
		}
		var expiry int64
		_, scanErr := fmt.Sscan(q.Get("expires_at"), &expiry)
		session := managedSession{q.Get("access_token"), q.Get("refresh_token"), expiry, q.Get("email")}
		if scanErr != nil || session.AccessToken == "" || session.RefreshToken == "" || session.Email == "" {
			http.Error(w, "Invalid session", 400)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fmt.Fprint(w, "JAIPilot sign-in complete. You can close this tab.")
		select {
		case completed <- session:
		default:
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = server.Shutdown(closeCtx)
	}()
	u, _ := url.Parse(jaipilotSite + "/plugin-login")
	q := u.Query()
	q.Set("redirect_uri", "http://"+listener.Addr().String()+"/auth/callback")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	fmt.Fprintln(os.Stderr, "Complete JAIPilot sign-in:", u.String())
	go openManagedBrowser(ctx, u.String())
	select {
	case session := <-completed:
		if err := atomicJSON(managedSessionPath(), session, 0600); err != nil {
			return "", err
		}
		return session.AccessToken, nil
	case <-ctx.Done():
		return "", fmt.Errorf("sign-in cancelled or timed out: %w", ctx.Err())
	}
}
func openManagedBrowser(ctx context.Context, address string) {
	name, args := "xdg-open", []string{address}
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/c", "start", "", address}
	}
	if exec.CommandContext(ctx, name, args...).Run() != nil {
		fmt.Fprintln(os.Stderr, "Open the sign-in URL in a browser on this computer.")
	}
}
