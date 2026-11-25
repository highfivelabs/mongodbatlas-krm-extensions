// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokenURL is where Atlas exchanges a service account's client credentials
// for an access token (relative to the request's own host).
const tokenPath = "/api/oauth/token"

type token struct {
	value   string
	expires time.Time
}

var (
	tokensMu sync.Mutex
	tokens   = map[string]token{} // by host + client ID
	// tokenClient is replaceable in tests.
	tokenClient = &http.Client{Timeout: 30 * time.Second}
)

// atlasAuth authenticates as an Atlas service account: it trades clientId
// and clientSecret for a bearer token, and reuses the token until shortly
// before it expires.
func atlasAuth(r *http.Request, c map[string]string) error {
	key := r.URL.Host + "|" + c["clientId"]
	tokensMu.Lock()
	t, ok := tokens[key]
	tokensMu.Unlock()
	if !ok || time.Now().After(t.expires) {
		var err error
		if t, err = fetchToken(r.URL, c["clientId"], c["clientSecret"]); err != nil {
			return err
		}
		tokensMu.Lock()
		tokens[key] = t
		tokensMu.Unlock()
	}
	r.Header.Set("Authorization", "Bearer "+t.value)
	return nil
}

func fetchToken(base *url.URL, id, secret string) (token, error) {
	u := url.URL{Scheme: base.Scheme, Host: base.Host, Path: tokenPath}
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return token{}, err
	}
	req.SetBasicAuth(id, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := tokenClient.Do(req)
	if err != nil {
		return token{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return token{}, fmt.Errorf("atlas token: %s: %s", resp.Status, data)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.AccessToken == "" {
		return token{}, fmt.Errorf("atlas token: unexpected response")
	}
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}
	return token{value: out.AccessToken, expires: time.Now().Add(life - time.Minute)}, nil
}

// appServicesLoginPath is where the App Services Admin API signs a
// programmatic API key in (relative to the request's own host): POST
// {"username": <public key>, "apiKey": <private key>} answers
// {"access_token": ..., "refresh_token": ...} (go.mongodb.org/realm v0.1.0
// auth.NewTokenFromCredentials, which terraform-provider-mongodbatlas
// v2.21.0 signs in with for mongodbatlas_event_trigger).
const appServicesLoginPath = "/api/admin/v3.0/auth/providers/mongodb-cloud/login"

// appServicesTokenLife: the login answers no lifetime, so its token is
// reused for at most this long, then the key signs in again.
const appServicesTokenLife = 10 * time.Minute

// appServicesAuth authenticates as an Atlas programmatic API key on the App
// Services Admin API (a Connection whose Secret holds publicKey and
// privateKey): it signs in and reuses the access token.
func appServicesAuth(r *http.Request, c map[string]string) error {
	key := "app-services|" + r.URL.Host + "|" + c["publicKey"]
	tokensMu.Lock()
	t, ok := tokens[key]
	tokensMu.Unlock()
	if !ok || time.Now().After(t.expires) {
		var err error
		if t, err = appServicesLogin(r.URL, c["publicKey"], c["privateKey"]); err != nil {
			return err
		}
		tokensMu.Lock()
		tokens[key] = t
		tokensMu.Unlock()
	}
	r.Header.Set("Authorization", "Bearer "+t.value)
	return nil
}

func appServicesLogin(base *url.URL, publicKey, privateKey string) (token, error) {
	u := url.URL{Scheme: base.Scheme, Host: base.Host, Path: appServicesLoginPath}
	body, _ := json.Marshal(map[string]string{"username": publicKey, "apiKey": privateKey})
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(string(body)))
	if err != nil {
		return token{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := tokenClient.Do(req)
	if err != nil {
		return token{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return token{}, fmt.Errorf("app services login: %s: %s", resp.Status, data)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.AccessToken == "" {
		return token{}, fmt.Errorf("app services login: unexpected response")
	}
	return token{value: out.AccessToken, expires: time.Now().Add(appServicesTokenLife)}, nil
}
