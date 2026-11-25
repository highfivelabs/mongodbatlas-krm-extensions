// Copyright 2026 High Five Labs
// SPDX-License-Identifier: Apache-2.0

// The mongodbatlas-krm-extensions manager: one generic controller per generated kind.
// --groups limits it to some API groups.
package main

import (
	"github.com/highfivelabs/krm-rest-runtime/manager"
	extensions "github.com/highfivelabs/mongodbatlas-krm-extensions"
)

func main() {
	manager.Main(manager.Options{Vendor: extensions.Vendor, Types: extensions.Types, Hooks: extensions.Hooks})
}
