// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import "os"

func shutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
