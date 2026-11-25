// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAtlasAuthExchangesAndReusesTheToken(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tokenPath {
			t.Errorf("token request to %s", r.URL.Path)
		}
		if id, secret, _ := r.BasicAuth(); id != "cid" || secret != "csecret" {
			t.Errorf("basic auth %q/%q", id, secret)
		}
		calls++
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	}))
	defer srv.Close()
	creds := map[string]string{"clientId": "cid", "clientSecret": "csecret"}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/atlas/v2/groups", nil)
		if err := atlasAuth(req, creds); err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer tok" {
			t.Fatalf("Authorization = %q", got)
		}
	}
	if calls != 1 {
		t.Fatalf("token fetched %d times, want 1", calls)
	}
}

// A Connection whose Secret holds a programmatic API key signs in to the
// App Services Admin API and reuses the access token.
func TestAppServicesAuthSignsInWithTheKey(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != appServicesLoginPath {
			t.Errorf("login request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "pub" || body["apiKey"] != "priv" {
			t.Errorf("login body %v", body)
		}
		calls++
		_, _ = w.Write([]byte(`{"access_token":"as-tok","refresh_token":"r"}`))
	}))
	defer srv.Close()
	creds := map[string]string{"publicKey": "pub", "privateKey": "priv"}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/v3.0/groups/g/apps/a/triggers", nil)
		if err := atlasAuthorize(req, creds); err != nil {
			t.Fatal(err)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer as-tok" {
			t.Fatalf("Authorization = %q", got)
		}
	}
	if calls != 1 {
		t.Fatalf("signed in %d times, want 1", calls)
	}
}
