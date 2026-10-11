package api

import "testing"

func TestValidateMailmapAccepts(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"blank lines only", "\n\n   \n"},
		{"comments", "# a comment\n   # indented comment\n"},
		{"merge two addresses", "alice <alice@example.com> <alice.personal@example.org>\n"},
		{"merge without a name", "<canonical@example.com> <other@example.com>\n"},
		// Single-address form only corrects a display name, but it is valid
		// mailmap and useful for tidying bot identities — must not be rejected.
		{"rename only", "Houston CI <bot@ci>\n"},
		{"name and email on both sides", "Proper Name <proper@e.com> Commit Name <commit@e.com>\n"},
		{"mixed with comments and blanks", "# who\n\nalice <a@b> <c@d>\nHouston CI <bot@ci>\n"},
		{"no trailing newline", "alice <a@b> <c@d>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMailmap(tc.in); err != nil {
				t.Fatalf("validateMailmap(%q) = %v, want nil", tc.in, err)
			}
		})
	}
}

func TestValidateMailmapRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"no address at all", "alice\n"},
		{"bare email without brackets", "alice alice@example.com\n"},
		{"unclosed angle bracket", "alice <a@b\n"},
		{"stray closing bracket", "alice a@b>\n"},
		{"empty brackets", "alice <>\n"},
		{"nested opening bracket", "alice <<a@b>>\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateMailmap(tc.in); err == nil {
				t.Fatalf("validateMailmap(%q) = nil, want an error", tc.in)
			}
		})
	}
}

// The whole point of rejecting rather than writing is telling the admin which
// line to fix, since git itself would ignore the bad line in silence.
func TestValidateMailmapReportsOffendingLine(t *testing.T) {
	in := "# header\n\nalice <a@b> <c@d>\noops no address here\n"
	err := validateMailmap(in)
	if err == nil {
		t.Fatal("expected an error for the 4th line")
	}
	if got := err.Error(); got[:7] != "line 4:" {
		t.Fatalf("error = %q, want it to start with %q", got, "line 4:")
	}
}
