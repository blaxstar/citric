package model

import "time"

type BuildStatus string

const (
	BuildStatusQueued  BuildStatus = "queued"
	BuildStatusRunning BuildStatus = "running"
	BuildStatusPass    BuildStatus = "pass"
	BuildStatusFail    BuildStatus = "fail"
)

type Build struct {
	ID       int64       `json:"id"`
	CLID     int64       `json:"cl_id"`
	PSID     int64       `json:"ps_id"`
	ToolName string      `json:"tool_name"`
	Status   BuildStatus `json:"status"`
	LogPath  string      `json:"log_path,omitempty"`
	// Node is the build node that ran it; empty when the server ran it itself.
	Node      string     `json:"node,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
