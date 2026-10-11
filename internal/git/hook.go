package git

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// PushEvent is parsed from the post-receive hook's stdin lines.
// Each line is: "<old-sha> <new-sha> <ref-name>"
type PushEvent struct {
	OldSHA string
	NewSHA string
	Ref    string
}

// ParsePostReceive reads the stdin payload written by git's post-receive hook.
func ParsePostReceive(r io.Reader) ([]PushEvent, error) {
	var events []PushEvent
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 3 {
			continue
		}
		events = append(events, PushEvent{
			OldSHA: parts[0],
			NewSHA: parts[1],
			Ref:    parts[2],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse post-receive: %w", err)
	}
	return events, nil
}

// ChangeIDFromCommit extracts the Change-Id footer from a commit message.
// Returns "" if not present.
func ChangeIDFromCommit(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Change-Id:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Change-Id:"))
		}
	}
	return ""
}
