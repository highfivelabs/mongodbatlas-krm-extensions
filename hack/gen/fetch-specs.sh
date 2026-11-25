#!/usr/bin/env bash
# Copyright 2026 High Five Labs
# SPDX-License-Identifier: Apache-2.0
# Download the vendor's OpenAPI specs at the commits vendor.yaml pins.
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p specs
[ -s specs/mongodbatlas.yaml ] || curl -fsSL -o specs/mongodbatlas.yaml "https://raw.githubusercontent.com/mongodb/openapi/5133085a709d9bfe2db3a14f4964a04c831a3c8c/openapi/v2.yaml"
