package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
)

const MaxArtifactBytes = 16 * 1024 * 1024

var artifactRoute = regexp.MustCompile(`^/v1/world-runs/cw_[a-f0-9]{32}/evidence/([0-9]|1[0-5])/(report|snapshot|evidence|trace)$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Artifact reads immutable bytes and verifies the manifest's size and digest.
// It uses the same no-redirect transport, never forwards credentials to storage,
// and makes one GET attempt. Error text and artifact bodies never enter logs.
func (c *Client) Artifact(ctx context.Context, path string, size int, digest string) ([]byte, error) {
	if !artifactRoute.MatchString(path) || size < 1 || size > MaxArtifactBytes || !digestPattern.MatchString(digest) {
		return nil, &Error{Code: "invalid_artifact_identity"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+path, nil)
	if err != nil {
		return nil, &Error{Code: "invalid_request"}
	}
	req.Close = true
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &Error{Code: "http_error", Status: response.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(size)+1))
	if err != nil {
		return nil, transportError(err)
	}
	hash := sha256.Sum256(data)
	if len(data) != size || hex.EncodeToString(hash[:]) != digest {
		return nil, &Error{Code: "artifact_integrity_error"}
	}
	return data, nil
}
