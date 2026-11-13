module github.com/garamsh/garam/gaon-operator

go 1.27.4

require (
	// The PM moves this with GAON_COMMIT in the Makefile, to the same
	// commit of gaon, in the same pull request.
	github.com/garamsh/gaon/lib/api v0.0.0-20261113042354-2387cebd0b44
	github.com/garamsh/garam/lib/errorx v0.0.0-00010101000000-000000000000
	github.com/garamsh/garam/lib/grn v0.0.0-00010101000000-000000000000
	github.com/garamsh/garam/lib/platform-sdk-go v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
	go.yaml.in/yaml/v3 v3.0.5
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/oapi-codegen/runtime v1.8.0 // indirect
)

replace (
	github.com/garamsh/garam/lib/errorx => ../lib/errorx
	github.com/garamsh/garam/lib/grn => ../lib/grn
	github.com/garamsh/garam/lib/platform-sdk-go => ../lib/platform-sdk-go
)
