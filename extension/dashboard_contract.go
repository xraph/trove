package extension

import (
	"fmt"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	"github.com/xraph/forge"

	trovecontract "github.com/xraph/trove/extension/contract"
)

// buildDashboardContent builds the content service from config: the ticket
// signer, the route path and the upload cap.
func buildDashboardContent(cfg Config) (*trovecontract.Content, error) {
	c := &trovecontract.Content{Path: cfg.DashboardContentPath, MaxUploadBytes: cfg.DashboardMaxUploadBytes}
	if cfg.DashboardContentSecret != "" {
		s, err := trovecontract.NewSigner([]byte(cfg.DashboardContentSecret))
		if err != nil {
			return nil, err
		}
		c.Signer = s
		return c, nil
	}
	s, err := trovecontract.NewRandomSigner()
	if err != nil {
		return nil, err
	}
	c.Signer, c.PerProcessSecret = s, true
	return c, nil
}

// setupDashboard builds the store resolver and the content service, and
// mounts the content route. It runs at the end of Register, once every
// store is open.
func (e *Extension) setupDashboard(fapp forge.App, stores *trovecontract.Stores) error {
	content, err := buildDashboardContent(e.config)
	if err != nil {
		return fmt.Errorf("trove: dashboard content: %w", err)
	}
	e.dashStores, e.dashContent = stores, content
	if content.PerProcessSecret && e.Logger() != nil {
		e.Logger().Warn("trove: dashboard_content_secret is not set; content links work only while every request reaches this instance")
	}
	if router := fapp.Router(); router != nil {
		if err := router.Handle(content.Path, content.Handler(stores, e.Logger())); err != nil {
			return fmt.Errorf("trove: mount dashboard content route at %s: %w", content.Path, err)
		}
	}
	return nil
}

// RegisterContractContributor registers the trove contract contributor,
// which is what the React dashboard reads.
func (e *Extension) RegisterContractContributor(
	disp *dispatcher.Dispatcher,
	reg dashcontract.Registry,
	wreg dashcontract.WardenRegistry,
) error {
	if e.dashStores == nil || e.dashContent == nil {
		if logger := e.Logger(); logger != nil {
			logger.Warn("trove: not initialised; skipping contract contributor registration")
		}
		return nil
	}
	deps := trovecontract.Deps{Stores: e.dashStores, Content: e.dashContent}
	if logger := e.Logger(); logger != nil {
		deps.Logger = logger
	}
	if err := trovecontract.Register(disp, reg, wreg, deps); err != nil {
		return fmt.Errorf("trove: register contract contributor: %w", err)
	}
	return nil
}
