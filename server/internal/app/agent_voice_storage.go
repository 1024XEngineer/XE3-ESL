package app

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
)

var (
	errAgentVoiceObjectStorageOrigin = errors.New(
		"bootstrap: Agent voice object storage origin is invalid",
	)
	agentVoiceBucketPattern = regexp.MustCompile(
		`\A[a-z0-9][a-z0-9-]{1,61}[a-z0-9]\z`,
	)
)

// AgentVoiceObjectReadAllowedHosts derives the only signed-URL hosts that the
// server may fetch. Production callers must use the already validated,
// non-secret ObjectStorageConfig; tests using a Fake store supply their own
// explicit allowlist through RuntimeAudioConfiguration.
func AgentVoiceObjectReadAllowedHosts(
	storageConfig config.ObjectStorageConfig,
) ([]string, error) {
	if !storageConfig.Enabled {
		return nil, nil
	}
	origin := storageConfig.Endpoint
	endpoint, err := url.Parse(strings.TrimSpace(origin))
	if err != nil ||
		endpoint.Scheme != "https" ||
		endpoint.Host == "" ||
		(endpoint.Path != "" && endpoint.Path != "/") ||
		endpoint.User != nil ||
		endpoint.RawQuery != "" ||
		endpoint.Fragment != "" ||
		net.ParseIP(endpoint.Hostname()) != nil ||
		strings.HasSuffix(endpoint.Hostname(), ".") {
		return nil, errAgentVoiceObjectStorageOrigin
	}
	if storageConfig.Provider == config.ObjectStorageProviderQiniuKodo {
		return []string{strings.ToLower(endpoint.Host)}, nil
	}
	if !agentVoiceBucketPattern.MatchString(storageConfig.Bucket) {
		return nil, errAgentVoiceObjectStorageOrigin
	}
	endpointHost := strings.ToLower(endpoint.Host)
	bucketHost := strings.ToLower(
		storageConfig.Bucket + "." + endpoint.Hostname(),
	)
	if endpoint.Port() != "" {
		bucketHost = net.JoinHostPort(bucketHost, endpoint.Port())
	}
	return []string{bucketHost, endpointHost}, nil
}
