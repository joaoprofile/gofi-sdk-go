package oci

import (
	"testing"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/msq"
)

func TestConfigFrom(t *testing.T) {
	c := configFrom(msq.ProviderConfig{OCI: msq.OCICredentials{
		AuthMode: "workload_identity", Region: "sa-saopaulo-1", TenancyID: "ten", UserID: "usr", Fingerprint: "aa:bb", PrivateKey: "pem",
	}}).Credentials
	if c.AuthMode != cloudoci.AuthWorkloadIdentity || c.Region != "sa-saopaulo-1" ||
		c.TenancyID != "ten" || c.UserID != "usr" || c.Fingerprint != "aa:bb" || c.PrivateKey != "pem" {
		t.Errorf("credentials not mapped: %+v", c)
	}
}
