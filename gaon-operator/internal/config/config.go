// Package config reads the operator's settings from its environment.
// It holds addresses and file paths only: a credential is a file the
// deployment mounts, never a value in the environment.
package config

import (
	"strings"

	"github.com/garamsh/garam/lib/errorx"
)

// Config is what the operator is deployed with (ADR-0151 §6).
type Config struct {
	// GaonURL, GaonCAFile, GaonClientCertFile and GaonClientKeyFile
	// reach gaon's resource API, which authenticates the operator by a
	// client certificate.
	GaonURL            string
	GaonCAFile         string
	GaonClientCertFile string
	GaonClientKeyFile  string
	// GaramMachineURL and GaramRootFile reach garam's machine listener.
	GaramMachineURL string
	GaramRootFile   string
	// GaramTokenURL is the token URL the key issuance answered, the
	// assertion's audience.
	GaramTokenURL string
	// ControlURL and ControlRootFile reach the control service, and
	// ControlGRN is its operator GRN, the audience of the tokens it is
	// shown.
	ControlURL      string
	ControlRootFile string
	ControlGRN      string
	// OperatorKeyFile and OperatorClientID are the operator's own
	// identity for the client-assertion exchange. The client id is the
	// manager's operator GRN, which the control service is also told as
	// the controller.
	OperatorKeyFile  string
	OperatorClientID string
	// AgentImage is the composed guest image of this release.
	AgentImage string
	// HealthAddr is where the probes listen. It defaults to
	// DefaultHealthAddr.
	HealthAddr string
}

// DefaultHealthAddr is the health listener's address when HEALTH_ADDR
// is unset.
const DefaultHealthAddr = ":8081"

type setting struct {
	name string
	dest func(*Config) *string
}

var settings = []setting{
	{"GAON_API_URL", func(c *Config) *string { return &c.GaonURL }},
	{"GAON_CA_FILE", func(c *Config) *string { return &c.GaonCAFile }},
	{"GAON_CLIENT_CERT_FILE", func(c *Config) *string { return &c.GaonClientCertFile }},
	{"GAON_CLIENT_KEY_FILE", func(c *Config) *string { return &c.GaonClientKeyFile }},
	{"GARAM_MACHINE_URL", func(c *Config) *string { return &c.GaramMachineURL }},
	{"GARAM_ROOT_FILE", func(c *Config) *string { return &c.GaramRootFile }},
	{"GARAM_TOKEN_URL", func(c *Config) *string { return &c.GaramTokenURL }},
	{"CONTROL_URL", func(c *Config) *string { return &c.ControlURL }},
	{"CONTROL_ROOT_FILE", func(c *Config) *string { return &c.ControlRootFile }},
	{"CONTROL_GRN", func(c *Config) *string { return &c.ControlGRN }},
	{"OPERATOR_KEY_FILE", func(c *Config) *string { return &c.OperatorKeyFile }},
	{"OPERATOR_CLIENT_ID", func(c *Config) *string { return &c.OperatorClientID }},
	{"AGENT_IMAGE", func(c *Config) *string { return &c.AgentImage }},
}

// Load reads every setting through getenv and names each one that is
// missing. The error carries variable names, never values.
func Load(getenv func(string) string) (Config, error) {
	var c Config
	var missing []string
	for _, s := range settings {
		v := strings.TrimSpace(getenv(s.name))
		if v == "" {
			missing = append(missing, s.name)
			continue
		}
		*s.dest(&c) = v
	}
	if len(missing) > 0 {
		return Config{}, errorx.New(errorx.KindInvalidArgument,
			"missing settings: "+strings.Join(missing, ", "))
	}
	c.HealthAddr = strings.TrimSpace(getenv("HEALTH_ADDR"))
	if c.HealthAddr == "" {
		c.HealthAddr = DefaultHealthAddr
	}
	return c, nil
}
