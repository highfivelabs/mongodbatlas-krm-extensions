// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/highfivelabs/krm-rest-runtime/descriptor"
	"sigs.k8s.io/yaml"
)

// atlasSpec is the Atlas Administration API's OpenAPI spec, which
// hack/gen/fetch-specs.sh downloads at the commit hack/gen/vendor.yaml pins
// (hack/ci.sh fetches it before the tests).
const atlasSpec = "hack/gen/specs/mongodbatlas.yaml"

// namedBySent are the kinds whose create answers a page of the list it
// adds to (links, results, totalCount), not the new entry: each is named in
// its path by a field its create sends (an access list entry by its
// ipAddress, a team member by the user's id, a private endpoint by its
// endpointId), so its ID is that field of the request body, which the test
// checks instead.
var namedBySent = map[string]string{
	"AccessListApiKey":                                      "the create answers the API key's access list; an entry is named by its ipAddress",
	"ServiceAccountAccessListEntry":                         "the create answers the service account's access list; an entry is named by its ipAddress",
	"ProjectServiceAccountAccessListEntry":                  "the create answers the service account's access list; an entry is named by its ipAddress",
	"CloudUserTeamAssignment":                               "the create answers the team's members; a member is named by the user's id it sends",
	"PrivatelinkEndpointServiceDataFederationOnlineArchive": "the create answers the project's private endpoints; one is named by the endpointId it sends",
}

// Every kind made by a call of the pinned Atlas spec takes its ID from a
// field that call's answer has (a slash path for one inside it). The fake
// API answers whatever field a descriptor names, so only the spec can show
// a wrong one (HFL-699): StreamWorkspace and StreamProcessor took status.id
// from "id", but Atlas answers _id and name and names them in its paths by
// name; a stream Private Link connection, an export bucket and an online
// archive answer _id; a new organization is answered inside
// {"organization": ...}.
func TestIDsAreFieldsOfTheCreateAnswer(t *testing.T) {
	data, err := os.ReadFile(atlasSpec)
	if os.IsNotExist(err) && os.Getenv("CI") == "" {
		t.Skipf("%s not fetched: run hack/gen/fetch-specs.sh", atlasSpec)
	}
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	f, err := descriptor.Parse(Types)
	if err != nil {
		t.Fatal(err)
	}
	checked := map[string]bool{}
	for i := range f.Kinds {
		k := &f.Kinds[i]
		if k.Create == nil || k.ID == nil || k.ID.IsTemplate() {
			continue
		}
		op, _ := dig(spec, "paths", k.Create.Path, strings.ToLower(k.Create.Method)).(map[string]any)
		if op == nil {
			continue // a call of another API (App Services) or a path the spec lacks
		}
		props := createAnswer(spec, op)
		if props == nil {
			continue // the spec gives the answer no fields
		}
		checked[k.Kind] = true
		if why, ok := namedBySent[k.Kind]; ok {
			if _, page := props["results"]; !page {
				t.Errorf("%s: allowed because %s, but %s %s's answer is no page", k.Kind, why, k.Create.Method, k.Create.Path)
			}
			if _, ok := fieldAt(spec, requestBody(spec, op), k.ID.From); !ok {
				t.Errorf("%s: allowed because %s, but id.from %q is not a field of %s %s's body", k.Kind, why, k.ID.From, k.Create.Method, k.Create.Path)
			}
			continue
		}
		if names, ok := fieldAt(spec, props, k.ID.From); !ok {
			t.Errorf("%s: id.from %q is not a field of %s %s's answer (%s): set overrides.idFrom in hack/gen/vendor.yaml",
				k.Kind, k.ID.From, k.Create.Method, k.Create.Path, strings.Join(names, ", "))
		}
		if strings.HasPrefix(k.Create.Path, streamsPath) && strings.HasSuffix(k.ID.Param, "Name") && k.ID.From != "name" {
			// {tenantName}, {processorName}: the path names it by its name,
			// not by the _id the answer has too.
			t.Errorf("%s: the path names it by {%s}, but id.from is %q, not name", k.Kind, k.ID.Param, k.ID.From)
		}
	}
	for kind := range namedBySent {
		if !checked[kind] {
			t.Errorf("namedBySent: %s is not made by a call of %s; remove it", kind, atlasSpec)
		}
	}
	for _, want := range []string{"StreamWorkspace", "StreamProcessor", "StreamPrivatelinkEndpoint", "StreamConnectionFailover",
		"CloudBackupSnapshotExportBucket", "OnlineArchive", "Organization"} {
		if !checked[want] {
			t.Errorf("%s was not checked against %s", want, atlasSpec)
		}
	}
}

// streamsPath is where Atlas Stream Processing's calls are.
const streamsPath = "/api/atlas/v2/groups/{groupId}/streams"

// fieldAt reports whether the slash path at is a field of props, following
// nested objects; where it is not, it gives the fields at the level it
// failed.
func fieldAt(spec map[string]any, props map[string]any, at string) ([]string, bool) {
	segs := strings.Split(at, "/")
	for i, seg := range segs {
		v, ok := props[seg]
		if !ok {
			names := make([]string, 0, len(props))
			for n := range props {
				names = append(names, n)
			}
			sort.Strings(names)
			return names, false
		}
		if i < len(segs)-1 {
			props = schemaProps(spec, v)
		}
	}
	return nil, true
}

// requestBody is the fields of a call's JSON body, or of one element where
// the body is an array.
func requestBody(spec, op map[string]any) map[string]any {
	content, _ := dig(deref(spec, op["requestBody"]), "content").(map[string]any)
	types := make([]string, 0, len(content))
	for ct := range content {
		if strings.HasSuffix(ct, "json") {
			types = append(types, ct)
		}
	}
	sort.Strings(types)
	for _, ct := range types {
		s, _ := deref(spec, dig(content, ct, "schema")).(map[string]any)
		if s["type"] == "array" {
			return schemaProps(spec, s["items"])
		}
		return schemaProps(spec, s)
	}
	return nil
}

// createAnswer is the fields of a call's first 2xx JSON answer (Atlas's
// versioned application/vnd.atlas.<date>+json), nil where the spec gives it
// no schema with fields.
func createAnswer(spec, op map[string]any) map[string]any {
	resp, _ := op["responses"].(map[string]any)
	codes := make([]string, 0, len(resp))
	for c := range resp {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		if !strings.HasPrefix(c, "2") {
			continue
		}
		r, _ := deref(spec, resp[c]).(map[string]any)
		content, _ := r["content"].(map[string]any)
		types := make([]string, 0, len(content))
		for ct := range content {
			if strings.HasSuffix(ct, "json") {
				types = append(types, ct)
			}
		}
		sort.Strings(types)
		for _, ct := range types {
			if s := dig(content, ct, "schema"); s != nil {
				return schemaProps(spec, s)
			}
		}
	}
	return nil
}

// schemaProps is a schema's properties, with those of its allOf parts.
func schemaProps(spec map[string]any, s any) map[string]any {
	m, _ := deref(spec, s).(map[string]any)
	if m == nil {
		return nil
	}
	var out map[string]any
	add := func(p map[string]any) {
		if p == nil {
			return
		}
		if out == nil {
			out = map[string]any{}
		}
		for n, v := range p {
			out[n] = v
		}
	}
	if all, ok := m["allOf"].([]any); ok {
		for _, a := range all {
			add(schemaProps(spec, a))
		}
	}
	p, _ := m["properties"].(map[string]any)
	add(p)
	return out
}

// deref follows a local $ref ("#/components/...").
func deref(spec map[string]any, v any) any {
	for {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		ref, ok := m["$ref"].(string)
		if !ok {
			return v
		}
		v = dig(spec, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...)
	}
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
