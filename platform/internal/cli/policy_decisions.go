package cli

import (
	"google.golang.org/grpc"

	"github.com/garamsh/garam/lib/errorx"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policydecision"
	policydecisiongrpc "github.com/garamsh/garam/platform/internal/policydecision/grpc"
	policyfeedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	policyrelaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
	"github.com/garamsh/garam/platform/internal/signer"
)

// dialPolicyDecisions connects the decision client a send is asked
// through, over the address and credentials the feed delivers with
// (ADR-0168). Unlike the feed, a send has no deployment without a
// policy service: nothing else decides it, so an unset address is
// refused here, by name, rather than every send failing later. The
// returned function closes the connection.
func dialPolicyDecisions() (policydecision.Service, func(), error) {
	conn, err := dialPolicyConn()
	if err != nil {
		return nil, nil, err
	}
	return policydecisiongrpc.New(policyv1.NewPolicyServiceClient(conn)),
		func() { _ = conn.Close() }, nil
}

// dialPolicyClients connects, over one connection, the decision client
// and the client that relays an organization's role links, guardrails
// and permits to the policy service (ADR-0169). The returned function
// closes the connection.
func dialPolicyClients() (policydecision.Service, policyrelay.Service, func(), error) {
	conn, err := dialPolicyConn()
	if err != nil {
		return nil, nil, nil, err
	}
	client := policyv1.NewPolicyServiceClient(conn)
	return policydecisiongrpc.New(client), policyrelaygrpc.New(client),
		func() { _ = conn.Close() }, nil
}

func dialPolicyConn() (*grpc.ClientConn, error) {
	cfg, err := readPolicyDeliveryConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errorx.New(errorx.KindInvalidArgument,
			string(envPolicyAddr)+" is required: the policy service decides every send and every policy write")
	}
	key, err := signer.LoadKey(cfg.keyFile)
	if err != nil {
		return nil, errorx.Wrap(err, string(envPolicySigningKeyFile))
	}
	return policyfeedgrpc.Dial(cfg.addr, cfg.caFile, cfg.issuer, key)
}
