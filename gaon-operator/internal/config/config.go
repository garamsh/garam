// Package config reads the operator's settings from its environment.
// It holds addresses and file paths only: a credential is a file the
// deployment mounts, never a value in the environment.
package config

import (
	"regexp"
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
	// GuestMachineURL and GuestControlURL are the addresses the adapter
	// in the guest reaches garam and the control service at. Each
	// defaults to the operator's own address for the same service.
	GuestMachineURL string
	GuestControlURL string
	// ModelKeyEnv, ModelKeySecret and ModelKeySecretKey name where the
	// model's credential comes from: the variable the runtime reads
	// and the Secret key the deployment created for it. All three or
	// none; none renders an agent with no model credential.
	ModelKeyEnv       string
	ModelKeySecret    string
	ModelKeySecretKey string
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
	c.GuestMachineURL = optional(getenv, "GUEST_MACHINE_URL", c.GaramMachineURL)
	c.GuestControlURL = optional(getenv, "GUEST_CONTROL_URL", c.ControlURL)
	if err := c.loadModelKey(getenv); err != nil {
		return Config{}, err
	}
	c.HealthAddr = strings.TrimSpace(getenv("HEALTH_ADDR"))
	if c.HealthAddr == "" {
		c.HealthAddr = DefaultHealthAddr
	}
	return c, nil
}

// optional returns the setting name, or fallback where it is unset.
func optional(getenv func(string) string, name, fallback string) string {
	if v := strings.TrimSpace(getenv(name)); v != "" {
		return v
	}
	return fallback
}

var (
	// variableName is a name the runtime can read from its environment.
	variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// secretKey is a Secret key gaon accepts.
	secretKey = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)
)

// guestPrefix marks the supervisor's own settings, which a model key
// must not take the name of.
const guestPrefix = "GUEST_"

// loadModelKey reads the three model key settings and refuses a set
// that is partial or names something the guest cannot carry. The error
// names variables, never values.
func (c *Config) loadModelKey(getenv func(string) string) error {
	c.ModelKeyEnv = strings.TrimSpace(getenv("MODEL_KEY_ENV"))
	c.ModelKeySecret = strings.TrimSpace(getenv("MODEL_KEY_SECRET"))
	c.ModelKeySecretKey = strings.TrimSpace(getenv("MODEL_KEY_SECRET_KEY"))
	set := 0
	for _, v := range []string{c.ModelKeyEnv, c.ModelKeySecret, c.ModelKeySecretKey} {
		if v != "" {
			set++
		}
	}
	switch {
	case set == 0:
		return nil
	case set != 3:
		return errorx.New(errorx.KindInvalidArgument,
			"MODEL_KEY_ENV, MODEL_KEY_SECRET and MODEL_KEY_SECRET_KEY are set together or not at all")
	case !variableName.MatchString(c.ModelKeyEnv) || strings.HasPrefix(c.ModelKeyEnv, guestPrefix):
		return errorx.New(errorx.KindInvalidArgument,
			"MODEL_KEY_ENV is not a variable name the runtime may read")
	case !secretKey.MatchString(c.ModelKeySecretKey) || c.ModelKeySecretKey == "." || c.ModelKeySecretKey == "..":
		return errorx.New(errorx.KindInvalidArgument, "MODEL_KEY_SECRET_KEY is not a Secret key")
	}
	return nil
}
