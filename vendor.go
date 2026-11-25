// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"encoding/json"
	"net/http"

	"github.com/highfivelabs/krm-rest-runtime/rest"
)

// Vendor is how the manager reaches MongoDB Atlas.
var Vendor = &rest.Vendor{
	Name:           "mongodbatlas",
	DefaultBaseURL: "https://cloud.mongodb.com",
	CredentialKeys: []string{"clientId", "clientSecret"},
	// The App Services Admin API (EventTrigger, on a Connection of its own
	// to https://services.cloud.mongodb.com) signs in with a programmatic
	// API key instead: its Secret holds publicKey and privateKey.
	AltCredentialKeys: [][]string{{"publicKey", "privateKey"}},
	// A service account's client credentials are exchanged for a bearer
	// token (auth.go); each kind's operations name their API version.
	// atlasAuthorize also cleans up the org-level service account when a
	// ProjectServiceAccount is deleted (hooks.go).
	AuthorizeE: atlasAuthorize,
	// Atlas refuses a request without a versioned media type (HTTP 406
	// INVALID_VERSION_DATE). Calls with none of their own (the live
	// sweep's lists and deletes) name the newest version date the kinds
	// use; Atlas answers each with the latest version on or before it.
	Headers:  map[string]string{"Accept": "application/vnd.atlas.2025-03-12+json"},
	NotFound: notFound,
}

// notFoundCodes are Atlas error codes that mean the object does not exist
// though the status is not 404: a read of a deleted API key answers 400
// API_KEY_NOT_FOUND.
var notFoundCodes = map[string]bool{"API_KEY_NOT_FOUND": true}

func notFound(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	var e struct {
		ErrorCode string `json:"errorCode"`
	}
	return json.Unmarshal(body, &e) == nil && notFoundCodes[e.ErrorCode]
}
