package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// Fingerprint returns the SHA-256 fingerprint of an SSH public key
// in the same format as `ssh-keygen -lf` (e.g. "SHA256:abc...").
func Fingerprint(pubkeyLine string) (string, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubkeyLine))
	if err != nil {
		return "", fmt.Errorf("parse ssh key: %w", err)
	}
	h := sha256.Sum256(pub.Marshal())
	return "SHA256:" + base64.StdEncoding.EncodeToString(h[:]), nil
}
