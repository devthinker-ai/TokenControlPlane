package upstream

// DeviceEndpoints are non-standard device-flow paths for known authorization
// servers. Extend as we onboard quirky MCP vendors; unknown servers fall back
// to RFC 8628 defaults (/device_authorization + /token).
type DeviceEndpoints struct {
	DeviceAuth string
	Token      string
	ClientID   string
	Scopes     string
}

// Known non-standard device endpoints (probed 2026-09-06).
var deviceEndpointHints = map[string]DeviceEndpoints{
	"fnf-device-auth.higgsfield.ai": {
		DeviceAuth: "/authorize",
		Token:      "/token",
		ClientID:   DefaultClientID,
		Scopes:     "openid email offline_access",
	},
}

// LookupDeviceHints returns endpoints for a known AS host, or false.
func LookupDeviceHints(host string) (DeviceEndpoints, bool) {
	h, ok := deviceEndpointHints[host]
	return h, ok
}
