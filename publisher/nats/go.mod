module github.com/getsyntegrity/ego/publisher/nats

go 1.26.0

require (
	github.com/getsyntegrity/ego v1.0.0
	github.com/nats-io/nats.go v1.53.1
	github.com/tochemey/gopack v0.2.1
	go.uber.org/multierr v1.11.0 // indirect
)

require (
	github.com/flowchartsman/retry v1.2.0
	go.uber.org/atomic v1.11.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/klauspost/compress v1.20.0 // indirect
	github.com/nats-io/nkeys v0.4.16 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// HashiCorp renamed github.com/armon/go-metrics to github.com/hashicorp/go-metrics
// in v0.4.2 and every release since declares the new module path, so they fail
// to satisfy the legacy import path that hashicorp/go-metrics/compat still
// pulls in transitively (via memberlist → goakt → ego). v0.4.1 is the last
// version that resolves under the armon path; exclude the broken ones so
// `go mod tidy` and `go get -u` stop probing them.
exclude (
	github.com/armon/go-metrics v0.4.2
	github.com/armon/go-metrics v0.5.0
	github.com/armon/go-metrics v0.5.1
	github.com/armon/go-metrics v0.5.2
	github.com/armon/go-metrics v0.5.3
	github.com/armon/go-metrics v0.5.4
	github.com/armon/go-metrics v0.6.0
	github.com/armon/go-metrics v0.6.1
)

replace github.com/getsyntegrity/ego => ../../
