// Package extension provides the Forge extension for Trove object storage.
//
// It integrates Trove with the Forge application framework, providing:
//   - Grove ORM models and store layer for metadata persistence
//   - Automatic database migrations
//   - REST API handlers for object, bucket, upload, CAS, and VFS operations
//   - Configuration via YAML or functional options
//   - DI integration (provides *trove.Trove to the container)
//   - Auto-discovery of ecosystem services (Chronicle, Vault, Dispatch, Warden)
//
// Usage:
//
//	app := forge.New()
//	app.Use(extension.New(
//	    extension.WithGroveDatabase("default"),
//	))
package extension
