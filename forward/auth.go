package forward

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const authURL = "https://bots.qq.com/app/getAppAccessToken"

type authRequest struct {
	AppID        string `json:"appId"`
	ClientSecret string `json:"clientSecret"`
}

type authResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// FetchAccessToken calls QQ Bot API to validate credentials and get a token.
func FetchAccessToken(appID, secret string) (accessToken string, expiresIn int, err error) {
	body, _ := json.Marshal(authRequest{
		AppID:        appID,
		ClientSecret: secret,
	})

	resp, err := http.Post(authURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	var result authResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return "", 0, fmt.Errorf("auth response parse failed: %w", err)
	}

	if result.AccessToken == "" {
		return "", 0, fmt.Errorf("invalid credentials: %s", string(data))
	}

	return result.AccessToken, result.ExpiresIn, nil
}

// TokenManager periodically refreshes QQ Bot access_token.
type TokenManager struct {
	appID  string
	secret string

	mu          sync.RWMutex
	accessToken string
}

func NewTokenManager(appID, secret string) *TokenManager {
	return &TokenManager{appID: appID, secret: secret}
}

// Token returns the current access_token (thread-safe).
func (tm *TokenManager) Token() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.accessToken
}

// SetInitialToken sets the token and schedules auto-refresh.
func (tm *TokenManager) SetInitialToken(token string, expiresIn int) {
	tm.mu.Lock()
	tm.accessToken = token
	tm.mu.Unlock()

	log.Printf("[auth] bot=%s token set, expires_in=%ds", tm.appID, expiresIn)
	tm.scheduleRefresh(expiresIn)
}

func (tm *TokenManager) scheduleRefresh(expiresIn int) {
	nextRefresh := time.Duration(float64(expiresIn)*0.8) * time.Second
	if nextRefresh < 60*time.Second {
		nextRefresh = 60 * time.Second
	}

	time.AfterFunc(nextRefresh, func() {
		if err := tm.refresh(); err != nil {
			log.Printf("[auth] bot=%s refresh error: %v, retrying in 30s", tm.appID, err)
			time.AfterFunc(30*time.Second, func() { _ = tm.refresh() })
		}
	})
}

func (tm *TokenManager) refresh() error {
	token, expiresIn, err := FetchAccessToken(tm.appID, tm.secret)
	if err != nil {
		return err
	}

	tm.mu.Lock()
	tm.accessToken = token
	tm.mu.Unlock()

	log.Printf("[auth] bot=%s token refreshed, expires_in=%ds", tm.appID, expiresIn)
	tm.scheduleRefresh(expiresIn)

	return nil
}
