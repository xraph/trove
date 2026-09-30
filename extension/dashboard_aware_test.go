package extension

import (
	dashboard "github.com/xraph/forge/extensions/dashboard"
)

// The dashboard finds contract contributors by type assertion at runtime,
// so production code never imports forge's dashboard root. This keeps the
// method checked against the real interface anyway.
var _ dashboard.ContractContributorAware = (*Extension)(nil)
