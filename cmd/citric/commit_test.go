package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildCommitMessage(t *testing.T) {
	tests := []struct {
		name       string
		subject    string
		paragraphs []string
		want       string
	}{
		{
			name:    "subject only",
			subject: "fix the thing",
			want:    "fix the thing",
		},
		{
			name:       "subject and one paragraph",
			subject:    "fix the thing",
			paragraphs: []string{"It was broken."},
			want:       "fix the thing\n\nIt was broken.",
		},
		{
			name:       "each paragraph separated by exactly one blank line",
			subject:    "fix the thing",
			paragraphs: []string{"First.", "Second.", "Third."},
			want:       "fix the thing\n\nFirst.\n\nSecond.\n\nThird.",
		},
		{
			// A stray -p "" must not open a double blank line, which would read
			// as an empty paragraph and split the body in git log.
			name:       "empty and whitespace paragraphs are dropped",
			subject:    "fix the thing",
			paragraphs: []string{"First.", "", "   ", "\n", "Second."},
			want:       "fix the thing\n\nFirst.\n\nSecond.",
		},
		{
			// Newlines inside one -p are the caller's own formatting (a list,
			// hard-wrapped prose) and must survive intact.
			name:       "newlines within a paragraph are preserved",
			subject:    "fix the thing",
			paragraphs: []string{"Because:\n  - one\n  - two"},
			want:       "fix the thing\n\nBecause:\n  - one\n  - two",
		},
		{
			name:       "surrounding whitespace is trimmed off each part",
			subject:    "  fix the thing\n",
			paragraphs: []string{"\n First. \n"},
			want:       "fix the thing\n\nFirst.",
		},
		{
			name:       "no subject falls back to paragraphs alone",
			paragraphs: []string{"Body only."},
			want:       "Body only.",
		},
		{
			name: "nothing in, nothing out",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildCommitMessage(tt.subject, tt.paragraphs, 0); got != tt.want {
				t.Errorf("buildCommitMessage(%q, %q, 0)\n got: %q\nwant: %q",
					tt.subject, tt.paragraphs, got, tt.want)
			}
		})
	}
}

func TestWithChangeID(t *testing.T) {
	const cid = "I7a80c5626d3820f0d36ed2aa45667b0e58e2908e"

	t.Run("appends when absent", func(t *testing.T) {
		got := withChangeID("subject\n\nbody.", cid)
		want := "subject\n\nbody.\n\nChange-Id: " + cid
		if got != want {
			t.Errorf("\n got: %q\nwant: %q", got, want)
		}
	})

	// The reword path feeds the previous message back in, and that message
	// already ends in a Change-Id. Appending unconditionally is how a commit
	// ends up carrying the trailer twice.
	t.Run("does not duplicate an existing trailer", func(t *testing.T) {
		msg := "subject\n\nbody.\n\nChange-Id: " + cid
		if got := withChangeID(msg, cid); got != msg {
			t.Errorf("message was modified:\n got: %q\nwant: %q", got, msg)
		}
	})

	t.Run("detects a trailer among other trailers", func(t *testing.T) {
		msg := "subject\n\nbody.\n\nCo-Authored-By: Someone <a@b.c>\nChange-Id: " + cid + "\n"
		if got := withChangeID(msg, cid); got != msg {
			t.Errorf("message was modified:\n got: %q\nwant: %q", got, msg)
		}
	})

	t.Run("does not stack blank lines on a trailing newline", func(t *testing.T) {
		got := withChangeID("subject\n\nbody.\n\n", cid)
		want := "subject\n\nbody.\n\nChange-Id: " + cid
		if got != want {
			t.Errorf("\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("empty change-id leaves the message alone", func(t *testing.T) {
		if got := withChangeID("subject", ""); got != "subject" {
			t.Errorf("got %q", got)
		}
	})
}

func TestWrapParagraph(t *testing.T) {
	const width = 20

	t.Run("wraps at the given width without breaking words", func(t *testing.T) {
		got := wrapParagraph("the quick brown fox jumps over the lazy dog", width)
		want := "the quick brown fox\njumps over the lazy\ndog"
		if got != want {
			t.Errorf("\n got: %q\nwant: %q", got, want)
		}
		for _, line := range strings.Split(got, "\n") {
			if len(line) > width {
				t.Errorf("line exceeds width %d: %q", width, line)
			}
		}
	})

	// Internal newlines mean the caller formatted the paragraph on purpose —
	// a list, an aligned block — and reflowing it would destroy that.
	t.Run("a paragraph with newlines is returned untouched", func(t *testing.T) {
		in := "Reasons this is a deliberately long line:\n  - one\n  - two"
		if got := wrapParagraph(in, width); got != in {
			t.Errorf("\n got: %q\nwant: %q", got, in)
		}
	})

	t.Run("width zero or negative disables wrapping", func(t *testing.T) {
		in := "the quick brown fox jumps over the lazy dog"
		for _, w := range []int{0, -1} {
			if got := wrapParagraph(in, w); got != in {
				t.Errorf("width %d: got %q, want unchanged", w, got)
			}
		}
	})

	// A URL or long path must stay on one line so it remains copy-pasteable.
	t.Run("a word longer than the width is never broken", func(t *testing.T) {
		long := "https://citric.example.com/cl/300/patchsets/4/diff"
		got := wrapParagraph("see "+long+" for details", width)
		lines := strings.Split(got, "\n")
		found := false
		for _, l := range lines {
			if l == long {
				found = true
			}
		}
		if !found {
			t.Errorf("long word was broken or merged: %q", got)
		}
	})

	// These messages are full of em dashes; counting their UTF-8 bytes would
	// wrap several columns early on exactly the lines that contain them.
	t.Run("width counts runes, not bytes", func(t *testing.T) {
		// Each "—" is 3 bytes but one column. Six of these words is 17 runes
		// and fits on one line of 20; by byte count it is 29 and would wrap.
		in := "a— b— c— d— e— f—"
		if n := utf8.RuneCountInString(in); n > width {
			t.Fatalf("test input is %d runes, must fit in %d", n, width)
		}
		if len(in) <= width {
			t.Fatalf("test input is %d bytes, must exceed %d to be meaningful", len(in), width)
		}
		if got := wrapParagraph(in, width); got != in {
			t.Errorf("wrapped early on multibyte runes:\n got: %q\nwant: %q", got, in)
		}
	})

	t.Run("subject line is never wrapped", func(t *testing.T) {
		subject := "a subject line that is considerably longer than the wrap width"
		got := buildCommitMessage(subject, nil, width)
		if got != subject {
			t.Errorf("\n got: %q\nwant: %q", got, subject)
		}
	})

	t.Run("wrapping applies through buildCommitMessage", func(t *testing.T) {
		got := buildCommitMessage("subj", []string{"the quick brown fox jumps over the lazy dog"}, width)
		want := "subj\n\nthe quick brown fox\njumps over the lazy\ndog"
		if got != want {
			t.Errorf("\n got: %q\nwant: %q", got, want)
		}
	})
}
