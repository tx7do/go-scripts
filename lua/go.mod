module github.com/tx7do/go-scripts/lua

go 1.25.0

replace github.com/tx7do/go-scripts => ../

require (
	github.com/stretchr/testify v1.11.1
	github.com/tengattack/gluacrypto v0.0.0-20240324200146-54b58c95c255
	github.com/tx7do/go-scripts v0.0.8
	github.com/vadv/gopher-lua-libs v0.8.0
	github.com/yuin/gluamapper v0.0.0-20150323120927-d836955830e7
	github.com/yuin/gopher-lua v1.1.2
	layeh.com/gopher-luar v1.0.11
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-kratos/kratos/v2 v2.9.2 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/tx7do/kratos-bootstrap/api v0.0.44 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

require (
	github.com/aws/aws-sdk-go v1.55.8 // indirect
	github.com/cbroglie/mustache v1.4.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-sql-driver/mysql v1.10.0 // indirect
	github.com/lib/pq v1.12.3 // indirect
	github.com/mattn/go-sqlite3 v1.14.48 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.24.1 // indirect
	github.com/tx7do/go-scripts/hostmodule v0.0.1
	github.com/tx7do/go-utils/crypto v0.0.4
	github.com/tx7do/kratos-bootstrap/logger v0.1.3
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/tx7do/go-scripts/hostmodule => ../hostmodule
