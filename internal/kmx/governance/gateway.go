package governance

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/kaimahi-agents/kaimahi/agentsuite/policy"
)

// GatewayEgressProjection is deliberately not a deployable whole-policy Plan.
// TLS passthrough cannot inspect HTTP paths, methods, JWTs or A2A operations.
type GatewayEgressProjection struct {
	Scope        string              `json:"scope"`
	PolicyDigest string              `json:"policyDigest"`
	Config       EgressGatewayConfig `json:"config"`
	Unenforced   []string            `json:"unenforced"`
}

type EgressGatewayConfig struct {
	Binds     []ConnectBind         `json:"binds"`
	Gateways  map[string]TLSGateway `json:"gateways"`
	TCPRoutes []EgressRoute         `json:"tcpRoutes"`
}

type ConnectBind struct {
	Port           int        `json:"port"`
	TunnelProtocol string     `json:"tunnelProtocol"`
	Listeners      []struct{} `json:"listeners"`
}

type TLSGateway struct {
	Port      int           `json:"port"`
	Listeners []TLSListener `json:"listeners"`
}

type TLSListener struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Protocol string `json:"protocol"`
}

type EgressRoute struct {
	Name      string        `json:"name"`
	Gateways  string        `json:"gateways"`
	Hostnames []string      `json:"hostnames"`
	Backends  []HostBackend `json:"backends"`
}

// ProjectGatewayEgress lowers HTTPS authorities to CONNECT + SNI routes in
// agentgateway v1.6.0. Fixed backends prevent CONNECT targets or Host headers
// from redirecting an allowed route to a caller-selected upstream.
func ProjectGatewayEgress(document policy.Document) (GatewayEgressProjection, error) {
	if err := document.Validate(); err != nil {
		return GatewayEgressProjection{}, err
	}
	if len(document.Network.Allow) == 0 {
		return GatewayEgressProjection{}, errors.New("gateway egress projection requires an explicit HTTPS allowlist; use the deny-all gateway sample for an empty list")
	}
	if len(document.Invocations.Allow) != 0 {
		return GatewayEgressProjection{}, errors.New("A2A peer/skill grants require an identity and protocol-aware adapter; a TLS tunnel is not sufficient")
	}
	config := EgressGatewayConfig{
		Binds:     []ConnectBind{{Port: 3000, TunnelProtocol: "connect", Listeners: []struct{}{}}},
		Gateways:  map[string]TLSGateway{},
		TCPRoutes: []EgressRoute{},
	}
	destinations := slices.Clone(document.Network.Allow)
	slices.SortFunc(destinations, func(a, b policy.Destination) int {
		return strings.Compare(fmt.Sprintf("%s:%05d", a.Host, a.Port), fmt.Sprintf("%s:%05d", b.Host, b.Port))
	})
	for i, destination := range destinations {
		if destination.Scheme != "https" || destination.Port == 3000 {
			return GatewayEgressProjection{}, errors.New("gateway projection supports HTTPS only; destination port must not overlap the CONNECT listener on 3000")
		}
		name := fmt.Sprintf("tls-%d", destination.Port)
		config.Gateways[name] = TLSGateway{Port: destination.Port, Listeners: []TLSListener{{
			Name: "egress", Hostname: "*", Protocol: "TLS",
		}}}
		config.TCPRoutes = append(config.TCPRoutes, EgressRoute{
			Name: fmt.Sprintf("allow-%d", i), Gateways: name + "/egress",
			Hostnames: []string{destination.Host},
			Backends:  []HostBackend{{Host: net.JoinHostPort(destination.Host, strconv.Itoa(destination.Port))}},
		})
	}
	digest, err := Digest(document)
	if err != nil {
		return GatewayEgressProjection{}, err
	}
	return GatewayEgressProjection{
		Scope: "gateway-tls-authorities-only", PolicyDigest: digest, Config: config,
		Unenforced: []string{
			"filesystem write denial",
			"direct network/proxy bypass and DNS/IP restrictions",
			"caller identity and A2A peer/skill authorization, including default deny",
			"HTTP paths, methods and non-HTTP traffic inside allowed TLS tunnels",
		},
	}, nil
}
