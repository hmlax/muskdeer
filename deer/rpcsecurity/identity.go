package rpcsecurity

import (
	"context"
	"slices"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// Service identities are exact certificate DNS names, never request metadata.
const (
	ServiceAPI       = "api.grpc"
	ServiceMarket    = "market.grpc"
	ServiceStrategy  = "strategy.grpc"
	ServiceExecution = "execution.grpc"
	ServicePortfolio = "portfolio.grpc"
	ServiceCron      = "cron.grpc"
)

// HasService requires a verified mTLS peer with the exact requested DNS SAN.
// It supplements the listener allowlist with method-level service policy.
func HasService(ctx context.Context, service string) bool {
	if ctx == nil || !validService(service) {
		return false
	}
	remote, ok := peer.FromContext(ctx)
	if !ok {
		return false
	}
	identity, ok := remote.AuthInfo.(credentials.TLSInfo)
	return ok && len(identity.State.VerifiedChains) != 0 && len(identity.State.PeerCertificates) != 0 &&
		slices.Contains(identity.State.PeerCertificates[0].DNSNames, service)
}
