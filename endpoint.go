package opencodeauth

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
)

type canonicalAuthority struct {
	scheme       string
	host         string
	port         int
	explicitPort bool
	isLoopback   bool
}

func (a canonicalAuthority) equalOrigin(other canonicalAuthority) bool {
	return a.scheme == other.scheme && a.host == other.host && a.port == other.port
}

func (a canonicalAuthority) string() string {
	host := a.host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if a.explicitPort {
		return net.JoinHostPort(hostnameForJoin(host), strconv.Itoa(a.port))
	}
	return host
}

// hostnameForJoin removes brackets that are already present in an IPv6 host.
func hostnameForJoin(host string) string {
	return strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
}

type canonicalURL struct {
	u         url.URL
	authority canonicalAuthority
	path      string
}

func (u canonicalURL) String() string {
	return u.u.String()
}

func (u canonicalURL) endpoint(suffix string) string {
	copy := u.u
	copy.Path = u.path + suffix
	copy.RawPath = ""
	return copy.String()
}

func parseBaseURL(raw string) (canonicalURL, error) {
	if raw == "" {
		raw = DefaultBaseURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return canonicalURL{}, ErrInvalidConfiguration
	}
	if parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return canonicalURL{}, ErrInvalidConfiguration
	}
	authority, err := canonicalAuthorityForURL(parsed)
	if err != nil {
		return canonicalURL{}, ErrInvalidConfiguration
	}
	if authority.scheme == "http" && !authority.isLoopback {
		return canonicalURL{}, ErrInvalidConfiguration
	}
	basePath, ok := canonicalPath(parsed)
	if !ok {
		return canonicalURL{}, ErrInvalidConfiguration
	}
	parsed.Scheme = authority.scheme
	parsed.Host = authority.string()
	parsed.Path = basePath
	parsed.RawPath = ""
	parsed.ForceQuery = false
	return canonicalURL{u: *parsed, authority: authority, path: basePath}, nil
}

func canonicalPath(u *url.URL) (string, bool) {
	if u.RawPath != "" {
		return "", false
	}
	escaped := strings.ToLower(u.EscapedPath())
	for _, encoded := range []string{"%2f", "%5c", "%2e"} {
		if strings.Contains(escaped, encoded) {
			return "", false
		}
	}
	if strings.Contains(u.Path, "\\") {
		return "", false
	}
	trimmed := strings.TrimRight(u.Path, "/")
	if trimmed != "" {
		if path.Clean(trimmed) != trimmed {
			return "", false
		}
		for _, segment := range strings.Split(trimmed, "/") {
			if segment == "." || segment == ".." {
				return "", false
			}
		}
	}
	return trimmed, true
}

func canonicalAuthorityForURL(u *url.URL) (canonicalAuthority, error) {
	if u == nil || u.Scheme == "" || u.Host == "" {
		return canonicalAuthority{}, ErrInvalidConfiguration
	}
	return parseAuthority(strings.ToLower(u.Scheme), u.Host)
}

func parseAuthority(scheme, raw string) (canonicalAuthority, error) {
	if scheme != "http" && scheme != "https" || raw == "" || strings.ContainsAny(raw, "\x00\r\n\t /?#@") {
		return canonicalAuthority{}, ErrInvalidConfiguration
	}
	bracketed := strings.HasPrefix(raw, "[")
	hostPart, portText, explicitPort, ok := splitAuthority(raw)
	if !ok || hostPart == "" {
		return canonicalAuthority{}, ErrInvalidConfiguration
	}
	if strings.Contains(hostPart, "%") {
		return canonicalAuthority{}, ErrInvalidConfiguration
	}
	host := strings.ToLower(hostPart)
	var isLoopback bool
	if address, err := netip.ParseAddr(host); err == nil {
		if bracketed && !address.Is6() {
			return canonicalAuthority{}, ErrInvalidConfiguration
		}
		host = address.String()
		isLoopback = address.IsLoopback()
	} else {
		if bracketed {
			return canonicalAuthority{}, ErrInvalidConfiguration
		}
		if !validDNSName(host) {
			return canonicalAuthority{}, ErrInvalidConfiguration
		}
		isLoopback = host == "localhost"
	}
	port := 443
	if scheme == "http" {
		port = 80
	}
	if explicitPort {
		parsedPort, ok := parsePort(portText)
		if !ok {
			return canonicalAuthority{}, ErrInvalidConfiguration
		}
		port = parsedPort
	}
	return canonicalAuthority{scheme: scheme, host: host, port: port, explicitPort: explicitPort, isLoopback: isLoopback}, nil
}

func splitAuthority(raw string) (host, port string, explicitPort, ok bool) {
	if strings.HasPrefix(raw, "[") {
		close := strings.IndexByte(raw, ']')
		if close < 0 {
			return "", "", false, false
		}
		host = raw[1:close]
		rest := raw[close+1:]
		if rest == "" {
			return host, "", false, true
		}
		if !strings.HasPrefix(rest, ":") || len(rest) == 1 {
			return "", "", false, false
		}
		return host, rest[1:], true, true
	}
	if strings.Contains(raw, "]") || strings.Count(raw, ":") > 1 {
		return "", "", false, false
	}
	if colon := strings.IndexByte(raw, ':'); colon >= 0 {
		if colon == 0 || colon == len(raw)-1 {
			return "", "", false, false
		}
		return raw[:colon], raw[colon+1:], true, true
	}
	return raw, "", false, true
}

func parsePort(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	port, err := strconv.Atoi(value)
	return port, err == nil && port >= 1 && port <= 65535
}

func validDNSName(host string) bool {
	if host == "" || strings.HasSuffix(host, ".") || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			value := label[i]
			if (value < 'a' || value > 'z') && (value < '0' || value > '9') && value != '-' {
				return false
			}
		}
	}
	return true
}

type requestRoute uint8

const (
	routeModels requestRoute = iota + 1
	routeChat
	routeMessages
	routeResponses
)

func (c *Client) validateRequest(req *http.Request) (requestRoute, error) {
	if c == nil || req == nil || req.URL == nil {
		return 0, ErrDisallowedRequest
	}
	u := req.URL
	if u.Opaque != "" || u.User != nil || u.Fragment != "" || u.Host == "" || u.Scheme == "" {
		return 0, ErrDisallowedRequest
	}
	if _, ok := canonicalPath(u); !ok {
		return 0, ErrDisallowedRequest
	}
	authority, err := canonicalAuthorityForURL(u)
	if err != nil || !c.base.authority.equalOrigin(authority) {
		return 0, ErrDisallowedRequest
	}
	if req.Host != "" {
		hostAuthority, err := parseAuthority(authority.scheme, req.Host)
		if err != nil || !authority.equalOrigin(hostAuthority) {
			return 0, ErrDisallowedRequest
		}
	}
	basePath := c.base.path
	if u.Path == basePath+"/models" && req.Method == http.MethodGet {
		return routeModels, nil
	}
	if req.Method != http.MethodPost {
		return 0, ErrDisallowedRequest
	}
	switch u.Path {
	case basePath + "/chat/completions":
		return routeChat, nil
	case basePath + "/messages":
		return routeMessages, nil
	case basePath + "/responses":
		return routeResponses, nil
	default:
		return 0, ErrDisallowedRequest
	}
}
