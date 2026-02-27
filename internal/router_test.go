package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/internal"
)

func TestRouter_Resolve_Default(t *testing.T) {
	defaultDrv := memdriver.New()
	router := internal.NewRouter(defaultDrv)

	resolved := router.Resolve("bucket", "key")
	assert.Equal(t, defaultDrv, resolved)
}

func TestRouter_Resolve_PatternRoute(t *testing.T) {
	defaultDrv := memdriver.New()
	archiveDrv := memdriver.New()

	router := internal.NewRouter(defaultDrv)
	router.AddBackend("archive", archiveDrv)
	router.AddRoute("*.log", "archive")

	t.Run("matching pattern routes to named backend", func(t *testing.T) {
		resolved := router.Resolve("logs", "app.log")
		assert.Equal(t, archiveDrv, resolved)
	})

	t.Run("non-matching pattern routes to default", func(t *testing.T) {
		resolved := router.Resolve("data", "report.pdf")
		assert.Equal(t, defaultDrv, resolved)
	})
}

func TestRouter_Resolve_RouteFunc(t *testing.T) {
	defaultDrv := memdriver.New()
	complianceDrv := memdriver.New()

	router := internal.NewRouter(defaultDrv)
	router.AddBackend("compliance", complianceDrv)
	router.AddRouteFunc(func(bucket, _ string) string {
		if bucket == "compliance" {
			return "compliance"
		}
		return ""
	})

	t.Run("matching function routes to named backend", func(t *testing.T) {
		resolved := router.Resolve("compliance", "doc.pdf")
		assert.Equal(t, complianceDrv, resolved)
	})

	t.Run("non-matching function routes to default", func(t *testing.T) {
		resolved := router.Resolve("public", "image.png")
		assert.Equal(t, defaultDrv, resolved)
	})
}

func TestRouter_Resolve_FuncTakesPrecedence(t *testing.T) {
	defaultDrv := memdriver.New()
	funcDrv := memdriver.New()
	patternDrv := memdriver.New()

	router := internal.NewRouter(defaultDrv)
	router.AddBackend("func-backend", funcDrv)
	router.AddBackend("pattern-backend", patternDrv)
	router.AddRoute("*.log", "pattern-backend")
	router.AddRouteFunc(func(_, _ string) string {
		return "func-backend"
	})

	// RouteFunc should take precedence over pattern.
	resolved := router.Resolve("logs", "app.log")
	assert.Equal(t, funcDrv, resolved)
}

func TestRouter_Backend(t *testing.T) {
	defaultDrv := memdriver.New()
	namedDrv := memdriver.New()

	router := internal.NewRouter(defaultDrv)
	router.AddBackend("named", namedDrv)

	t.Run("existing backend", func(t *testing.T) {
		assert.Equal(t, namedDrv, router.Backend("named"))
	})

	t.Run("non-existing backend", func(t *testing.T) {
		assert.Nil(t, router.Backend("nonexistent"))
	})
}

func TestRouter_Default(t *testing.T) {
	defaultDrv := memdriver.New()
	router := internal.NewRouter(defaultDrv)

	assert.Equal(t, defaultDrv, router.Default())
}

func TestRouter_Backends(t *testing.T) {
	defaultDrv := memdriver.New()
	router := internal.NewRouter(defaultDrv)
	router.AddBackend("a", memdriver.New())
	router.AddBackend("b", memdriver.New())

	names := router.Backends()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "a")
	assert.Contains(t, names, "b")
}

func TestRouter_CloseAll(t *testing.T) {
	defaultDrv := memdriver.New()
	namedDrv := memdriver.New()

	router := internal.NewRouter(defaultDrv)
	router.AddBackend("named", namedDrv)

	closed := make(map[string]bool)
	err := router.CloseAll(func(d driver.Driver) error {
		closed[d.Name()] = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, closed["mem"])
}
