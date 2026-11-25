// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

package extensions

import (
	"testing"

	"github.com/highfivelabs/krm-rest-runtime/kindtest"
)

// Every kind, against the fake API built from types.yaml: create, update,
// drift repair, delete. It proves the descriptors and the controller agree;
// it does not prove MongoDB Atlas's API behaves as the descriptors say.
func TestEveryKindOnFake(t *testing.T) {
	v := *Vendor
	v.AuthorizeE = nil // the fake has no token endpoint; auth_test.go covers it
	// No Hooks: the fake's update step changes the first string field,
	// which for some kinds is one Atlas takes only on create (hooks.go,
	// covered by hooks_test.go and the live run).
	kindtest.EveryKind(t, Types, &v, nil)
}
