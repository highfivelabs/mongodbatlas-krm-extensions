// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/highfivelabs/krm-rest-runtime/descriptor"
	"github.com/highfivelabs/krm-rest-runtime/rest"
)

func TestCreateOnlyKindsExist(t *testing.T) {
	f, err := descriptor.Parse(Types)
	if err != nil {
		t.Fatal(err)
	}
	for kind, fields := range createOnly {
		var k *descriptor.Kind
		for i := range f.Kinds {
			if f.Kinds[i].Kind == kind {
				k = &f.Kinds[i]
			}
		}
		if k == nil || k.ID == nil || k.Update == nil {
			t.Errorf("createOnly names %s, which is not an updatable CRUD kind", kind)
		}
		_ = fields
	}
}

func TestDropCreateOnly(t *testing.T) {
	k := &descriptor.Kind{Kind: "OrgInvitation", ID: &descriptor.ID{Param: "invitationId"}}
	body := map[string]any{"username": "a@example.com", "roles": []any{"ORG_MEMBER"}}
	if err := DropCreateOnly(nil, nil, k, map[string]string{}, body); err != nil || body["username"] == nil {
		t.Fatalf("create: username dropped: %v %v", body, err)
	}
	if err := DropCreateOnly(nil, nil, k, map[string]string{"invitationId": "x"}, body); err != nil || body["username"] != nil || body["roles"] == nil {
		t.Fatalf("update: %v %v", body, err)
	}
}

func TestNotFound(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
		want   bool
	}{
		{400, `{"errorCode":"API_KEY_NOT_FOUND","error":400}`, true},
		{400, `{"errorCode":"INVALID_ATTRIBUTE","error":400}`, false},
		{500, `{"errorCode":"API_KEY_NOT_FOUND"}`, false},
		{400, `not json`, false},
	} {
		if got := notFound(c.status, []byte(c.body)); got != c.want {
			t.Errorf("notFound(%d, %s) = %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestParseProjectServiceAccountPath(t *testing.T) {
	for _, c := range []struct {
		path       string
		wantGroup  string
		wantClient string
		wantOK     bool
	}{
		{"/api/atlas/v2/groups/g1/serviceAccounts/mdb_sa_id_1", "g1", "mdb_sa_id_1", true},
		{"/api/atlas/v2/groups/g1/serviceAccounts/mdb_sa_id_1/", "g1", "mdb_sa_id_1", true},
		{"/api/atlas/v2/orgs/o1/serviceAccounts/mdb_sa_id_1", "", "", false},
		{"/api/atlas/v2/groups/g1/serviceAccounts", "", "", false},
		{"/api/atlas/v2/groups/g1/serviceAccounts/mdb_sa_id_1/secrets/s1", "", "", false},
		{"/api/atlas/v2/groups//serviceAccounts/mdb_sa_id_1", "", "", false},
	} {
		g, cid, ok := parseProjectServiceAccountPath(c.path)
		if g != c.wantGroup || cid != c.wantClient || ok != c.wantOK {
			t.Errorf("parseProjectServiceAccountPath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.path, g, cid, ok, c.wantGroup, c.wantClient, c.wantOK)
		}
	}
}

func TestProjectServiceAccountDeleteCleansUpOrgAccount(t *testing.T) {
	for _, tc := range []struct {
		name           string
		orgDeleteCode  int
		groupGetCode   int
		wantErr        bool
		wantOrgDeleted bool
	}{
		{"deletes org account", http.StatusNoContent, http.StatusOK, false, true},
		{"org account already gone", http.StatusNotFound, http.StatusOK, false, true},
		{"project-only credentials (403)", http.StatusForbidden, http.StatusOK, false, true},
		{"project already gone (404)", http.StatusNoContent, http.StatusNotFound, false, false},
		{"org delete server error", http.StatusInternalServerError, http.StatusOK, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			orgActive := true
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tokenPath {
					_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
					return
				}
				calls = append(calls, r.Method+" "+r.URL.Path)
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/atlas/v2/groups/grp1":
					w.WriteHeader(tc.groupGetCode)
					if tc.groupGetCode == http.StatusOK {
						_, _ = w.Write([]byte(`{"id":"grp1","orgId":"org1"}`))
					}
				case r.Method == http.MethodDelete && r.URL.Path == "/api/atlas/v2/orgs/org1/serviceAccounts/sa1":
					if tc.orgDeleteCode == http.StatusNoContent {
						orgActive = false
					}
					w.WriteHeader(tc.orgDeleteCode)
				case r.Method == http.MethodDelete && r.URL.Path == "/api/atlas/v2/groups/grp1/serviceAccounts/sa1":
					if !orgActive {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			c := rest.New(Vendor, srv.URL, map[string]string{"clientId": "cid", "clientSecret": "csecret"})
			_, err := c.DoWith(context.Background(), http.MethodDelete, "/api/atlas/v2/groups/grp1/serviceAccounts/sa1", nil, map[string]string{"Accept": serviceAccountAccept})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil && !errors.Is(err, rest.ErrNotFound) {
				t.Fatalf("unexpected error: %v", err)
			}
			gotOrgDelete := strings.Contains(strings.Join(calls, "\n"), "DELETE /api/atlas/v2/orgs/org1/serviceAccounts/sa1")
			if gotOrgDelete != tc.wantOrgDeleted {
				t.Fatalf("org delete called = %v, want %v (calls: %v)", gotOrgDelete, tc.wantOrgDeleted, calls)
			}
		})
	}
}

func TestAtlasAuthorizeIgnoresNonDelete(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"clientId":"sa1"}`))
	}))
	defer srv.Close()

	c := rest.New(Vendor, srv.URL, map[string]string{"clientId": "cid", "clientSecret": "csecret"})
	if _, err := c.Do(context.Background(), http.MethodGet, "/api/atlas/v2/groups/grp1/serviceAccounts/sa1", nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "GET /api/atlas/v2/groups/grp1/serviceAccounts/sa1" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

// A ServiceAccountProjectAssignment's delete removes the organization's
// account from the project only, and the marker header never reaches Atlas.
func TestAssignmentDeleteKeepsOrgAccount(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
			return
		}
		if r.Header.Get(keepOrgAccount) != "" {
			t.Errorf("%s reached Atlas", keepOrgAccount)
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := rest.New(Vendor, srv.URL, map[string]string{"clientId": "cid", "clientSecret": "csecret"})
	if _, err := c.DoWith(context.Background(), http.MethodDelete, "/api/atlas/v2/groups/grp1/serviceAccounts/sa1", nil,
		map[string]string{"Accept": serviceAccountAccept, keepOrgAccount: "true"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "DELETE /api/atlas/v2/groups/grp1/serviceAccounts/sa1" {
		t.Fatalf("calls: %v", calls)
	}
}
