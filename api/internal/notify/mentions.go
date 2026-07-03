package notify

import (
	"regexp"
	"strings"
)

// codeFenceRe strips fenced code blocks (```...```) before mention parsing,
// so a `@handle`-looking token pasted inside a code sample doesn't notify
// anyone. This is a text-level heuristic, not a markdown parser: it doesn't
// handle unbalanced or nested fences, and a body with an odd number of
// ``` markers will strip more (or less) than intended. Good enough for v1;
// documented rather than solved so a real markdown AST pass can replace it
// later without changing the mention-gating contract.
var codeFenceRe = regexp.MustCompile("(?s)```.*?```")

// inlineCodeRe strips inline code spans (`...`) for the same reason.
var inlineCodeRe = regexp.MustCompile("`[^`\n]*`")

// mentionRe matches an `@handle` at a word boundary. The alternation
// `(?:^|[^\w@])` requires the `@` to be preceded by start-of-string or a
// non-word, non-`@` character — which is what keeps an email address like
// `foo@example.com` from parsing as a mention of user "example": the `@` in
// an email is preceded by a word character (`o`), while a real mention's
// `@` is preceded by whitespace, punctuation, or nothing. This also means
// `foo@@bar` only ever matches the second `@` at most, and `@@bar` doesn't
// match at all — both reasonable, undocumented-elsewhere edge cases.
//
// The handle body (\w[\w-]{0,38}) is deliberately permissive — the schema
// puts no format CHECK on users.handle — capped at 39 characters total to
// bound backtracking. Known-accepted edge cases, not fixed further because
// v1 has no editor affordance that needs them:
//   - "\@handle" (markdown's literal-@ escape) still parses as a mention;
//     the escaping backslash is just another non-word preceding character.
//   - a handle immediately followed by a hyphen-joined word ("@alice-ish")
//     captures the whole thing as one handle, not "alice" plus literal
//     "-ish" — matches how GitHub treats hyphenated handles.
var mentionRe = regexp.MustCompile(`(?:^|[^\w@])@(\w[\w-]{0,38})`)

// ParseMentions extracts the distinct set of @handles referenced in body,
// lowercased (handle resolution is case-insensitive — see
// resolveMentionedUsers), in first-seen order, after stripping fenced and
// inline code spans.
func ParseMentions(body string) []string {
	stripped := codeFenceRe.ReplaceAllString(body, "")
	stripped = inlineCodeRe.ReplaceAllString(stripped, "")

	seen := make(map[string]bool)
	var handles []string
	for _, m := range mentionRe.FindAllStringSubmatch(stripped, -1) {
		h := strings.ToLower(m[1])
		if !seen[h] {
			seen[h] = true
			handles = append(handles, h)
		}
	}
	return handles
}
