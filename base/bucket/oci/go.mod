module github.com/joaoprofile/gofi-sdk-go/base/bucket/oci

go 1.26.6

require (
	github.com/joaoprofile/gofi-sdk-go/base v0.2.2
	github.com/joaoprofile/gofi-sdk-go/base/cloud/oci v0.2.2
	github.com/oracle/oci-go-sdk/v65 v65.112.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gofrs/flock v0.10.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sony/gobreaker v0.5.0 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/joaoprofile/gofi-sdk-go/base => ../..
	github.com/joaoprofile/gofi-sdk-go/base/cloud/oci => ../../cloud/oci
)
