module github.com/gopherex/xconf/contrib/sources/s3

go 1.25.7

require (
	github.com/aws/aws-sdk-go-v2 v1.47.1
	github.com/aws/aws-sdk-go-v2/service/s3 v1.113.4
	github.com/aws/smithy-go v1.28.1
	github.com/gopherex/schemapb/go v0.0.0-20260928000336-b5fc4d954ff4
	github.com/gopherex/xconf v1.2.2
)

require github.com/gopherex/xconf/contrib/decoders/json v1.2.2

require (
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.4 // indirect
	github.com/cbroglie/mustache v1.4.0 // indirect
	github.com/google/cel-go v0.30.0 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/gopherex/xconf => ../../..

replace github.com/gopherex/xconf/contrib/decoders/json => ../../decoders/json
