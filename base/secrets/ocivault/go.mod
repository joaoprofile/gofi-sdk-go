module github.com/gofi-labs/gofi-sdk-go/base/secrets/ocivault

go 1.26.6

require (
	github.com/gofi-labs/gofi-sdk-go/base v0.8.2
	github.com/gofi-labs/gofi-sdk-go/base/cloud/oci v0.8.2
	github.com/oracle/oci-go-sdk/v65 v65.112.0
)

require (
	github.com/gofrs/flock v0.10.0 // indirect
	github.com/sony/gobreaker v0.5.0 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace (
	github.com/gofi-labs/gofi-sdk-go/base => ../..
	github.com/gofi-labs/gofi-sdk-go/base/cloud/oci => ../../cloud/oci
)
