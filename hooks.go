// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/highfivelabs/krm-rest-runtime/descriptor"
	"github.com/highfivelabs/krm-rest-runtime/reconciler"
	"github.com/highfivelabs/krm-rest-runtime/rest"
)

// createOnly lists, by kind, the body fields Atlas takes when a resource is
// created and refuses on an update (HTTP 400 INVALID_ATTRIBUTE), as the live
// run found: a service account's first secret, an invitation's username, an
// AI model key's cloud and geography. A change to one is not applied; the
// object must be re-created.
var createOnly = map[string][]string{
	"ServiceAccount":        {"secretExpiresAfterHours", "withoutInitialSecret"},
	"ProjectServiceAccount": {"secretExpiresAfterHours", "withoutInitialSecret"},
	"OrgInvitation":         {"username"},
	"ProjectInvitation":     {"username"},
	"AiModelApiKey":         {"cloud", "geography"},
}

// Hooks drop each kind's create-only fields from the body once the resource
// exists, so an update sends only what Atlas can change.
var Hooks = func() map[string][]reconciler.Hook {
	out := map[string][]reconciler.Hook{}
	for kind := range createOnly {
		out[kind] = []reconciler.Hook{DropCreateOnly}
	}
	return out
}()

// DropCreateOnly removes k's create-only fields from an existing resource's
// body.
func DropCreateOnly(_ context.Context, _ *rest.Client, k *descriptor.Kind, params map[string]string, body map[string]any) error {
	if k.ID == nil || params[k.ID.Param] == "" {
		return nil // being created
	}
	for _, f := range createOnly[k.Kind] {
		delete(body, f)
	}
	return nil
}

const serviceAccountAccept = "application/vnd.atlas.2024-08-05+json"

// keepOrgAccount, on a DELETE of a project's service account, says the
// account is the organization's, invited to the project
// (ServiceAccountProjectAssignment): it is only removed from the project.
// The header is never sent to Atlas.
const keepOrgAccount = "X-Hfl-Keep-Org-Account"

// atlasAuthorize authenticates the request and, when deleting a
// ProjectServiceAccount, also deletes the underlying organization service
// account. Atlas's project-level DELETE only removes the service account from
// the project and leaves the org-level account active; krm-rest-runtime has no
// delete hook, so this runs on the Vendor.AuthorizeE path.
func atlasAuthorize(r *http.Request, c map[string]string) error {
	if c["clientId"] == "" && c["publicKey"] != "" {
		// A programmatic API key: the App Services Admin API's Connection
		// (EventTrigger).
		return appServicesAuth(r, c)
	}
	if err := atlasAuth(r, c); err != nil {
		return err
	}
	if r.Method == http.MethodDelete && r.Header.Get(keepOrgAccount) != "" {
		// A ServiceAccountProjectAssignment's delete: the account is the
		// organization's, only removed from the project.
		r.Header.Del(keepOrgAccount)
		return nil
	}
	if r.Method == http.MethodDelete {
		if groupID, clientID, ok := parseProjectServiceAccountPath(r.URL.Path); ok {
			return deleteOrgServiceAccount(r.Context(), r.URL, c, groupID, clientID)
		}
	}
	return nil
}

func parseProjectServiceAccountPath(path string) (groupID, clientID string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 7 ||
		parts[0] != "api" || parts[1] != "atlas" || parts[2] != "v2" ||
		parts[3] != "groups" || parts[4] == "" ||
		parts[5] != "serviceAccounts" || parts[6] == "" {
		return "", "", false
	}
	return parts[4], parts[6], true
}

func deleteOrgServiceAccount(ctx context.Context, base *url.URL, creds map[string]string, groupID, clientID string) error {
	orgID, err := lookupProjectOrgID(ctx, base, creds, groupID)
	if err != nil || orgID == "" {
		return err
	}
	u := url.URL{
		Scheme: base.Scheme,
		Host:   base.Host,
		Path:   fmt.Sprintf("/api/atlas/v2/orgs/%s/serviceAccounts/%s", orgID, clientID),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", serviceAccountAccept)
	req.Header.Set("User-Agent", "highfivelabs-mongodbatlas-extensions")
	if err := atlasAuth(req, creds); err != nil {
		return err
	}
	resp, err := tokenClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return nil
	case http.StatusForbidden:
		// Callers with Project Owner but not Organization Owner cannot delete
		// the org account; let the project-level delete proceed.
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("atlas org service account delete %s/%s: %s: %s", orgID, clientID, resp.Status, data)
	}
	return nil
}

func lookupProjectOrgID(ctx context.Context, base *url.URL, creds map[string]string, groupID string) (string, error) {
	u := url.URL{
		Scheme: base.Scheme,
		Host:   base.Host,
		Path:   fmt.Sprintf("/api/atlas/v2/groups/%s", groupID),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.atlas.2025-03-12+json")
	req.Header.Set("User-Agent", "highfivelabs-mongodbatlas-extensions")
	if err := atlasAuth(req, creds); err != nil {
		return "", err
	}
	resp, err := tokenClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("atlas group lookup %s: %s: %s", groupID, resp.Status, data)
	}
	var out struct {
		OrgID string `json:"orgId"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("atlas group lookup %s: %w", groupID, err)
	}
	return out.OrgID, nil
}
