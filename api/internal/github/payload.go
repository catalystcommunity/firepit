package github

import "encoding/json"

// OriginRef is the shape stored in posts.origin_ref / comments.origin_ref
// for every row this package writes — see the package doc comment for the
// full contract (dedup key, lookup key for issue/PR comment targeting).
type OriginRef struct {
	Repo       string `json:"repo"`
	Kind       string `json:"kind"` // "issue" | "pull_request" | "release"
	Number     int    `json:"number,omitempty"`
	URL        string `json:"url"`
	DeliveryID string `json:"delivery_id"`
}

// Marshal encodes ref as the jsonb bytes stored in origin_ref.
func (ref OriginRef) Marshal() ([]byte, error) {
	return json.Marshal(ref)
}

// repoOnlyPayload extracts just the repository identity common to every
// repo-scoped GitHub webhook payload — used to look up a mapping (and
// therefore a secret to verify against) before the rest of the payload is
// parsed or trusted.
type repoOnlyPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// peekRepoFullName extracts repository.full_name ("owner/name") from a raw
// webhook body without assuming any particular event type's shape.
func peekRepoFullName(body []byte) (string, error) {
	var p repoOnlyPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", err
	}
	return p.Repository.FullName, nil
}

// issuesEventPayload is the minimal subset of GitHub's "issues" webhook
// event schema this package needs (see
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#issues).
type issuesEventPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	} `json:"issue"`
}

// pullRequestEventPayload is the minimal subset of GitHub's "pull_request"
// webhook event schema this package needs (see
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request).
type pullRequestEventPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Merged  bool   `json:"merged"`
	} `json:"pull_request"`
}

// releaseEventPayload is the minimal subset of GitHub's "release" webhook
// event schema this package needs (see
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#release).
type releaseEventPayload struct {
	Action  string `json:"action"`
	Release struct {
		ID      int64  `json:"id"`
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
	} `json:"release"`
}

// firstNonEmpty returns the first non-empty string among vals, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// containsString reports whether needle is present in haystack.
func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
