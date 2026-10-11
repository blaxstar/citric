package model

import "time"

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleSubmitter Role = "submitter"
	RoleReviewer  Role = "reviewer"
	RoleReadonly  Role = "readonly"
)

type User struct {
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	SSHKey    string    `json:"ssh_key,omitempty"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}
