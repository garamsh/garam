module github.com/garamsh/garam/platform-cli

go 1.27.2

require (
	github.com/garamsh/garam/lib/errorx v0.0.0-00010101000000-000000000000
	github.com/garamsh/garam/lib/platform-sdk-go v0.0.0-00010101000000-000000000000
	github.com/google/uuid v1.6.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/oapi-codegen/runtime v1.8.0 // indirect
	github.com/rogpeppe/go-internal v1.15.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace (
	github.com/garamsh/garam/lib/errorx => ../lib/errorx
	github.com/garamsh/garam/lib/platform-sdk-go => ../lib/platform-sdk-go
)
