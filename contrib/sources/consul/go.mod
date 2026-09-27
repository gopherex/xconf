module github.com/gopherex/xconf/contrib/sources/consul

go 1.25.7

require (
	github.com/gopherex/schemapb/go v0.0.0-20260927201943-2e4130a88c4c
	github.com/gopherex/xconf v1.1.2
	github.com/gopherex/xconf/contrib/decoders/json v1.1.2
	github.com/hashicorp/consul/api v1.33.4
	golang.org/x/time v0.12.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/armon/go-metrics v0.4.1 // indirect
	github.com/cbroglie/mustache v1.4.0 // indirect
	github.com/fatih/color v1.16.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/cel-go v0.30.0 // indirect
	github.com/gopherex/xconf/contrib/sources/env v1.1.2
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.5.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/serf v0.10.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20250808145144-a408d31f581a // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/gopherex/xconf => ../../..

replace github.com/gopherex/xconf/contrib/decoders/json => ../../decoders/json

replace github.com/gopherex/xconf/contrib/sources/env => ../env
