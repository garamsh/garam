module github.com/garamsh/garam/gaon-operator

go 1.27.4

require (
	github.com/garamsh/gaon/lib/api v0.0.0-20261112212559-4d950576aa75
	github.com/garamsh/garam/lib/errorx v0.0.0-00010101000000-000000000000
	github.com/garamsh/garam/lib/grn v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.12.1
)

require go.yaml.in/yaml/v3 v3.0.5 // indirect

replace (
	github.com/garamsh/garam/lib/errorx => ../lib/errorx
	github.com/garamsh/garam/lib/grn => ../lib/grn
)
