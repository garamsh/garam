module github.com/garamsh/garam/lib/clisession

go 1.27.2

replace github.com/garamsh/garam/lib/errorx => ../errorx

require (
	github.com/garamsh/garam/lib/errorx v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect
