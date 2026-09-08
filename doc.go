// Package opencodeauth provides a small authenticated HTTP transport boundary
// for OpenCode Go coding-agent requests.
//
// The package validates the configured destination and decorates requests with
// the route-specific credentials and conversation identity required by the
// service. It does not encode native request formats or decode streaming
// responses; callers retain ordinary net/http ownership of those details.
package opencodeauth
