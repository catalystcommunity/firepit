package github

import _ "embed"

// Golden GitHub webhook payload fixtures (task B8 acceptance: "golden-payload
// tests using REAL GitHub webhook JSON fixtures ... hand-crafted to GitHub's
// documented schema"). Each is a full, realistic webhook body for one event
// type/action, trimmed to the fields this package (and GitHub's own docs at
// https://docs.github.com/en/webhooks/webhook-events-and-payloads) document
// as present. Exported so both this package's own tests and
// api/internal/server's webhook integration tests can drive the same
// fixtures without file-path fragility across packages.
var (
	//go:embed testdata/issues_opened.json
	FixtureIssuesOpened []byte

	//go:embed testdata/issues_closed.json
	FixtureIssuesClosed []byte

	//go:embed testdata/pull_request_merged.json
	FixturePullRequestMerged []byte

	//go:embed testdata/pull_request_closed_unmerged.json
	FixturePullRequestClosedUnmerged []byte

	//go:embed testdata/release_published.json
	FixtureReleasePublished []byte

	//go:embed testdata/ping.json
	FixturePing []byte

	//go:embed testdata/issues_opened_unmapped_repo.json
	FixtureIssuesOpenedUnmappedRepo []byte
)
