package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// dial returns a connection to seabird-core which authenticates with token.
// It connects lazily, so every wait is bounded by the context of the RPC or
// stream using it, and an unreachable core fails the first call quickly.
func dial(host, token string) (*grpc.ClientConn, error) {
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse host: %w", err)
	}

	var transport credentials.TransportCredentials
	port := u.Port()
	switch u.Scheme {
	case "http":
		transport = insecure.NewCredentials()
		if port == "" {
			port = "80"
		}
	case "https":
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificates: %w", err)
		}
		transport = credentials.NewClientTLSFromCert(pool, "")
		if port == "" {
			port = "443"
		}
	default:
		return nil, fmt.Errorf("unsupported scheme %q in host, expected http or https", u.Scheme)
	}

	conn, err := grpc.NewClient(net.JoinHostPort(u.Hostname(), port),
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(tokenAuth{token: token, secure: u.Scheme == "https"}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", host, err)
	}
	return conn, nil
}

// tokenAuth sends the auth token as a bearer token on every call.
type tokenAuth struct {
	token  string
	secure bool
}

func (a tokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + a.token}, nil
}

func (a tokenAuth) RequireTransportSecurity() bool {
	return a.secure
}
