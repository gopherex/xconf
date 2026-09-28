module github.com/gopherex/xconf/contrib/sources/nats

go 1.25.7

require (
	github.com/gopherex/schemapb/go v0.0.0-20260928135944-4f0aa1af98e5
	github.com/gopherex/xconf v1.2.3
	github.com/nats-io/nats.go v1.52.0
)

require (
	github.com/gopherex/xconf/contrib/decoders/json v1.2.3
	github.com/nats-io/nats-server/v2 v2.14.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.7.0-default-no-op // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/cbroglie/mustache v1.4.0 // indirect
	github.com/google/cel-go v0.30.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/nats-io/jwt/v2 v2.8.1 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/gopherex/xconf => ../../..

replace github.com/gopherex/xconf/contrib/decoders/json => ../../decoders/json
