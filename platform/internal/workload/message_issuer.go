package workload

import (
	"bytes"

	"gopkg.in/yaml.v3"

	"github.com/garamsh/garam/lib/errorx"
)

// How the runtime trusts garam's message signatures (ADR-0101, step 4):
// one issuers entry naming garam's issuer, the key set's URL, and the
// garam server root that URL is verified by. The runtime fetches the
// set itself and fetches it again on a key it does not hold, which
// covers a rotation, a first key and a key retired at once, so the
// supervisor renders the entry once and refreshes nothing.
const (
	// runtimeServerRoot is where the runtime holds the garam server
	// root, the one the adapter verifies garam by.
	runtimeServerRoot = "/etc/sherlock/garam-server-root.pem"
	// keySetPath is garam's key set on its machine listener.
	keySetPath = "/message-signing-keys"
)

// withIssuer answers the host's runtime configuration with garam's
// issuers entry added. The issuer is garam's identity, compared byte
// for byte; the URL is only where the runtime reaches the set, which is
// the adapter's address for garam, since the runtime shares its network
// namespace (ADR-0087). The host's own entries stay, so garam is one
// issuer among any the runtime trusts; one already naming garam's
// issuer refuses, since the runtime refuses two entries for one issuer
// at startup.
func withIssuer(config []byte, issuer, keysURL, caFile string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(config, &doc); err != nil {
		return nil, errorx.Wrap(err, "workload: the runtime configuration", errorx.KindInvalidArgument)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: the runtime configuration is not a mapping")
	}
	root := doc.Content[0]
	var list *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "issuers" {
			list = root.Content[i+1]
		}
	}
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "issuers"}, list)
	}
	if list.Kind != yaml.SequenceNode {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: the runtime configuration's issuers is not a list")
	}
	for _, entry := range list.Content {
		for i := 0; i+1 < len(entry.Content); i += 2 {
			if entry.Content[i].Value == "issuer" && entry.Content[i+1].Value == issuer {
				return nil, errorx.New(errorx.KindInvalidArgument,
					"workload: the runtime configuration already names garam's issuer "+issuer+"; the supervisor renders that entry")
			}
		}
	}
	scalar := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }
	list.Content = append(list.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		scalar("issuer"), scalar(issuer), scalar("keys-url"), scalar(keysURL), scalar("keys-ca-file"), scalar(caFile),
	}})
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, errorx.Wrap(err, "workload: the runtime configuration", errorx.KindInternal)
	}
	if err := enc.Close(); err != nil {
		return nil, errorx.Wrap(err, "workload: the runtime configuration", errorx.KindInternal)
	}
	return out.Bytes(), nil
}
