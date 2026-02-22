module github.com/Prescott-Data/nexus-framework/nexus-bridge

go 1.23.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/prometheus/client_golang v1.23.2
	google.golang.org/grpc v1.66.0
)

require (
	github.com/Prescott-Data/nexus-framework/nexus-sdk v0.0.0-local
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/aws/smithy-go v1.20.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240604185151-ef581f913117 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/Prescott-Data/nexus-framework/nexus-sdk => ../nexus-sdk
