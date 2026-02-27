package middleware

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScopeGlobal(t *testing.T) {
	s := ScopeGlobal{}
	ctx := context.Background()
	assert.True(t, s.Match(ctx, "any-bucket", "any-key"))
	assert.True(t, s.Match(ctx, "", ""))
	assert.Equal(t, "global", s.String())
}

func TestScopeBucket(t *testing.T) {
	s := &ScopeBucket{Buckets: []string{"images", "docs"}}
	ctx := context.Background()

	assert.True(t, s.Match(ctx, "images", "file.png"))
	assert.True(t, s.Match(ctx, "docs", "readme.md"))
	assert.False(t, s.Match(ctx, "logs", "app.log"))
	assert.False(t, s.Match(ctx, "", ""))
	assert.Contains(t, s.String(), "bucket(")
}

func TestScopeKeyPattern(t *testing.T) {
	s := &ScopeKeyPattern{Patterns: []string{"*.pdf", "images/*"}}
	ctx := context.Background()

	assert.True(t, s.Match(ctx, "any", "report.pdf"))
	assert.True(t, s.Match(ctx, "any", "images/hero.png"))
	assert.False(t, s.Match(ctx, "any", "report.doc"))
	assert.False(t, s.Match(ctx, "any", "videos/hero.mp4"))
	assert.Contains(t, s.String(), "key(")
}

func TestScopeFunc(t *testing.T) {
	called := false
	s := &ScopeFunc{
		Fn: func(_ context.Context, bucket, _ string) bool {
			called = true
			return bucket == "special"
		},
		Desc: "test-func",
	}
	ctx := context.Background()

	assert.True(t, s.Match(ctx, "special", "any"))
	assert.True(t, called)
	assert.False(t, s.Match(ctx, "other", "any"))
	assert.Equal(t, "test-func", s.String())
}

func TestScopeFunc_NoDesc(t *testing.T) {
	s := &ScopeFunc{Fn: func(_ context.Context, _, _ string) bool { return true }}
	assert.Equal(t, "func", s.String())
}

func TestScopeAnd(t *testing.T) {
	ctx := context.Background()
	s := &ScopeAnd{
		Scopes: []Scope{
			&ScopeBucket{Buckets: []string{"docs"}},
			&ScopeKeyPattern{Patterns: []string{"*.pdf"}},
		},
	}

	assert.True(t, s.Match(ctx, "docs", "report.pdf"))
	assert.False(t, s.Match(ctx, "docs", "report.doc"))
	assert.False(t, s.Match(ctx, "images", "report.pdf"))
	assert.Contains(t, s.String(), "and(")
}

func TestScopeAnd_Empty(t *testing.T) {
	s := &ScopeAnd{Scopes: []Scope{}}
	assert.True(t, s.Match(context.Background(), "any", "any"))
}

func TestScopeOr(t *testing.T) {
	ctx := context.Background()
	s := &ScopeOr{
		Scopes: []Scope{
			&ScopeBucket{Buckets: []string{"docs"}},
			&ScopeKeyPattern{Patterns: []string{"*.pdf"}},
		},
	}

	assert.True(t, s.Match(ctx, "docs", "readme.md"))
	assert.True(t, s.Match(ctx, "images", "report.pdf"))
	assert.True(t, s.Match(ctx, "docs", "report.pdf"))
	assert.False(t, s.Match(ctx, "images", "hero.png"))
	assert.Contains(t, s.String(), "or(")
}

func TestScopeOr_Empty(t *testing.T) {
	s := &ScopeOr{Scopes: []Scope{}}
	assert.False(t, s.Match(context.Background(), "any", "any"))
}

func TestScopeNot(t *testing.T) {
	ctx := context.Background()
	s := &ScopeNot{Inner: &ScopeBucket{Buckets: []string{"logs"}}}

	assert.True(t, s.Match(ctx, "docs", "any"))
	assert.False(t, s.Match(ctx, "logs", "any"))
	assert.Contains(t, s.String(), "not(")
}

func TestComposedScopes(t *testing.T) {
	ctx := context.Background()

	// (bucket=docs AND key=*.pdf) OR bucket=compliance
	s := Or(
		And(ForBuckets("docs"), ForKeys("*.pdf")),
		ForBuckets("compliance"),
	)

	assert.True(t, s.Match(ctx, "docs", "report.pdf"))
	assert.True(t, s.Match(ctx, "compliance", "anything"))
	assert.False(t, s.Match(ctx, "docs", "report.doc"))
	assert.False(t, s.Match(ctx, "images", "photo.pdf"))
}

func TestNot_WithAnd(t *testing.T) {
	ctx := context.Background()

	// NOT (bucket=logs AND key=*.tmp)
	s := Not(And(ForBuckets("logs"), ForKeys("*.tmp")))

	assert.True(t, s.Match(ctx, "docs", "file.txt"))
	assert.True(t, s.Match(ctx, "logs", "file.log"))
	assert.False(t, s.Match(ctx, "logs", "file.tmp"))
}
