package notify

import (
	"reflect"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "simple mention",
			body: "hey @alice, take a look",
			want: []string{"alice"},
		},
		{
			name: "mention at start of body",
			body: "@alice can you review this?",
			want: []string{"alice"},
		},
		{
			name: "case insensitive dedupe",
			body: "cc @Alice and also @alice again",
			want: []string{"alice"},
		},
		{
			name: "multiple distinct handles, first-seen order",
			body: "@bob please loop in @alice and @carol",
			want: []string{"bob", "alice", "carol"},
		},
		{
			name: "email address is not a mention",
			body: "contact me at foo@example.com for details",
			want: nil,
		},
		{
			name: "mention immediately after punctuation",
			body: "(@alice) said so; see (@bob).",
			want: []string{"alice", "bob"},
		},
		{
			name: "hyphenated handle captured whole",
			body: "ping @jane-doe please",
			want: []string{"jane-doe"},
		},
		{
			name: "fenced code block ignored",
			body: "before\n```\nemail @alice for creds\n```\nafter @bob",
			want: []string{"bob"},
		},
		{
			name: "inline code span ignored",
			body: "use `@alice` as an example, but really @bob",
			want: []string{"bob"},
		},
		{
			name: "bare @ with no handle does not match",
			body: "look at this @ sign",
			want: nil,
		},
		{
			name: "no mentions",
			body: "just a plain comment",
			want: nil,
		},
		{
			name: "markdown-escaped mention still parses (documented edge case)",
			body: `not intended as a mention: \@alice`,
			want: []string{"alice"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseMentions(tc.body)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseMentions(%q) = %#v, want %#v", tc.body, got, tc.want)
			}
		})
	}
}
