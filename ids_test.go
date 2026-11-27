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

// streamsPath is where Atlas Stream Processing's calls are.
const streamsPath = "/api/atlas/v2/groups/{groupId}/streams"

// Every Atlas Stream Processing kind takes its ID from a field its create's
// answer has. The fake API answers whatever field a descriptor names, so only
// the spec can show a wrong one: StreamWorkspace and StreamProcessor took
// status.id from "id", but Atlas answers _id and name and names them in its
// paths by name; StreamPrivatelinkEndpoint is named by its _id (HFL-699).
func TestStreamIDsAreFieldsOfTheCreateAnswer(t *testing.T) {
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
		if k.Create == nil || k.ID == nil || k.ID.IsTemplate() || !strings.HasPrefix(k.Create.Path, streamsPath) {
			continue
		}
		op, _ := dig(spec, "paths", k.Create.Path, strings.ToLower(k.Create.Method)).(map[string]any)
		if op == nil {
			t.Errorf("%s: %s %s is not in %s", k.Kind, k.Create.Method, k.Create.Path, atlasSpec)
			continue
		}
		props := createAnswer(spec, op)
		if props == nil {
			t.Errorf("%s: %s %s answers no fields in %s", k.Kind, k.Create.Method, k.Create.Path, atlasSpec)
			continue
		}
		checked[k.Kind] = true
		from := strings.Split(k.ID.From, "/")[0]
		if _, ok := props[from]; !ok {
			names := make([]string, 0, len(props))
			for n := range props {
				names = append(names, n)
			}
			sort.Strings(names)
			t.Errorf("%s: id.from %q is not a field of %s %s's answer (%s): set overrides.idFrom in hack/gen/vendor.yaml",
				k.Kind, k.ID.From, k.Create.Method, k.Create.Path, strings.Join(names, ", "))
		}
		if strings.HasSuffix(k.ID.Param, "Name") && from != "name" {
			// {tenantName}, {processorName}: the path names it by its name,
			// not by the _id the answer has too.
			t.Errorf("%s: the path names it by {%s}, but id.from is %q, not name", k.Kind, k.ID.Param, k.ID.From)
		}
	}
	for _, want := range []string{"StreamWorkspace", "StreamProcessor", "StreamPrivatelinkEndpoint", "StreamConnectionFailover"} {
		if !checked[want] {
			t.Errorf("%s was not checked against %s", want, atlasSpec)
		}
	}
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
