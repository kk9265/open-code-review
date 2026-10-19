// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package tool

import "context"

type reviewTargetsKey struct{}

func WithReviewTargets(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, reviewTargetsKey{}, append([]string(nil), paths...))
}

func ReviewTargets(ctx context.Context) []string {
	paths, _ := ctx.Value(reviewTargetsKey{}).([]string)
	return append([]string(nil), paths...)
}
