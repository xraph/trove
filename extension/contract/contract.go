// Package contract wires Trove into the Forge dashboard's contract path. It
// registers the `trove` contributor and answers its intents from the live
// Trove stores and their drivers, never from the extension's metadata
// store, which normal operation does not write.
package contract

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

//go:embed manifest.yaml
var manifestYAML []byte

// ContributorName is the join key with packages/plugin-trove's `extension`
// field and matches the extension's name. A mismatch hides the React
// plugin with no error anywhere.
const ContributorName = "trove"

// Deps bundles what the handlers need.
type Deps struct {
	// Stores resolves each intent's `store` field. Required.
	Stores *Stores

	// Content mints the tickets the content intents return. Required.
	Content *Content

	// Logger receives an Error entry for every error mapped to
	// CodeInternal. Optional.
	Logger forge.Logger
}

// binding registers one intent with the dispatcher.
type binding struct {
	intent string
	bind   func(d *dispatcher.Dispatcher) error
}

func query[I, O any](intent string, fn func(context.Context, I, contract.Principal) (O, error)) binding {
	return binding{intent: intent, bind: func(d *dispatcher.Dispatcher) error {
		return dispatcher.RegisterQuery(d, ContributorName, intent, 1, fn)
	}}
}

func command[I, O any](intent string, fn func(context.Context, I, contract.Principal) (O, error)) binding {
	return binding{intent: intent, bind: func(d *dispatcher.Dispatcher) error {
		return dispatcher.RegisterCommand(d, ContributorName, intent, 1, fn)
	}}
}

// bindings lists every intent this package answers.
func bindings(deps Deps) []binding {
	return []binding{
		query("system.status", systemStatusHandler(deps)),
		query("stores.list", storesListHandler(deps)),
		query("middleware.list", middlewareListHandler(deps)),
		query("buckets.list", bucketsListHandler(deps)),
		command("buckets.create", bucketsCreateHandler(deps)),
		command("buckets.delete", bucketsDeleteHandler(deps)),
		query("objects.list", objectsListHandler(deps)),
		query("objects.head", objectsHeadHandler(deps)),
		command("objects.delete", objectsDeleteHandler(deps)),
		command("objects.copy", objectsCopyHandler(deps)),
		command("objects.presign", objectsPresignHandler(deps)),
		query("objects.contentUrl", objectsContentURLHandler(deps)),
		command("objects.beginUpload", objectsBeginUploadHandler(deps)),
		command("objects.completeUpload", objectsCompleteUploadHandler(deps)),
	}
}

// Register loads and validates the embedded manifest, registers the
// `trove` contributor with reg, and binds every handler. A handler bound to
// an intent the manifest does not declare is a build mistake and fails
// here rather than at the first request.
func Register(
	d *dispatcher.Dispatcher,
	reg contract.Registry,
	wreg contract.WardenRegistry,
	deps Deps,
) error {
	if deps.Stores == nil {
		return fmt.Errorf("trove/contract: Stores is required")
	}
	if deps.Content == nil || deps.Content.Signer == nil {
		return fmt.Errorf("trove/contract: Content with a Signer is required")
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "trove/contract/manifest.yaml")
	if err != nil {
		return fmt.Errorf("trove/contract: load manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("trove/contract: validate manifest: %w", err)
	}
	if err := reg.Register(m); err != nil {
		return fmt.Errorf("trove/contract: register manifest: %w", err)
	}

	declared := make(map[string]bool, len(m.Intents))
	for _, in := range m.Intents {
		declared[in.Name] = true
	}
	for _, b := range bindings(deps) {
		if !declared[b.intent] {
			return fmt.Errorf("trove/contract: %s is bound but the manifest does not declare it", b.intent)
		}
		if err := b.bind(d); err != nil {
			return fmt.Errorf("trove/contract: register %s: %w", b.intent, err)
		}
	}
	return nil
}
