// Package ocivault resolves secret://ocivault/<secret-ocid>[#key] and
// secret://ocivault/<vault-ocid>/<secret-name>[#key] from OCI Vault.
//
// OCI has no implicit credential chain, so enable it explicitly before the
// environment loads, typically in main:
//
//	ocivault.Register(ocivault.Config{Credentials: cloudoci.Config{AuthMode: cloudoci.AuthWorkloadIdentity}})
package ocivault

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	cloudoci "github.com/gofi-labs/gofi-sdk-go/base/cloud/oci"
	"github.com/gofi-labs/gofi-sdk-go/base/secrets"
	"github.com/oracle/oci-go-sdk/v65/common"
	ocisecrets "github.com/oracle/oci-go-sdk/v65/secrets"
)

// Provider is the reference provider name.
const Provider = "ocivault"

// Config selects the identity; Region defaults to the one in each OCID.
type Config struct {
	Credentials cloudoci.Config
	// Endpoint overrides the service host (tests, dedicated endpoints).
	Endpoint string
}

// Register enables the provider for secret references; call it once, a second
// call panics (see secrets.Register).
func Register(cfg Config) {
	secrets.Register(Provider, func(context.Context) (secrets.Store, error) {
		return New(cfg)
	})
}

// Store reads secret bundles; it keeps one client per region.
type Store struct {
	cfg      Config
	provider common.ConfigurationProvider

	mu      sync.Mutex
	clients map[string]ocisecrets.SecretsClient
}

// New validates the identity; clients are created on first use.
func New(cfg Config) (*Store, error) {
	p, err := cloudoci.ConfigurationProvider(cfg.Credentials)
	if err != nil {
		return nil, err
	}
	return &Store{cfg: cfg, provider: p, clients: map[string]ocisecrets.SecretsClient{}}, nil
}

// Get returns the current version of a secret by OCID or <vault-ocid>/<name>.
func (s *Store) Get(ctx context.Context, name string) (string, error) {
	vault, secretName, byName := strings.Cut(name, "/")
	ocid := name
	if byName {
		ocid = vault
	}
	client, err := s.client(regionOf(ocid))
	if err != nil {
		return "", err
	}
	var content ocisecrets.SecretBundleContentDetails
	if byName {
		resp, err := client.GetSecretBundleByName(ctx, ocisecrets.GetSecretBundleByNameRequest{SecretName: &secretName, VaultId: &vault})
		if err != nil {
			return "", mapErr(err)
		}
		content = resp.SecretBundleContent
	} else {
		resp, err := client.GetSecretBundle(ctx, ocisecrets.GetSecretBundleRequest{SecretId: &ocid})
		if err != nil {
			return "", mapErr(err)
		}
		content = resp.SecretBundleContent
	}
	b64, ok := content.(ocisecrets.Base64SecretBundleContentDetails)
	if !ok || b64.Content == nil {
		return "", fmt.Errorf("ocivault: %s: unsupported bundle content %T", name, content)
	}
	raw, err := base64.StdEncoding.DecodeString(*b64.Content)
	if err != nil {
		return "", fmt.Errorf("ocivault: %s: decode: %w", name, err)
	}
	return string(raw), nil
}

func (s *Store) client(region string) (ocisecrets.SecretsClient, error) {
	if s.cfg.Credentials.Region != "" {
		region = s.cfg.Credentials.Region
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[region]; ok {
		return c, nil
	}
	c, err := ocisecrets.NewSecretsClientWithConfigurationProvider(s.provider)
	if err != nil {
		return c, fmt.Errorf("ocivault: client: %w", err)
	}
	if region != "" {
		c.SetRegion(region)
	}
	if s.cfg.Endpoint != "" {
		c.Host = s.cfg.Endpoint
	}
	s.clients[region] = c
	return c, nil
}

// regionOf reads the region segment of ocid1.<type>.<realm>.<region>.<id>.
func regionOf(ocid string) string {
	parts := strings.Split(ocid, ".")
	if len(parts) >= 5 {
		return parts[3]
	}
	return ""
}

func mapErr(err error) error {
	var svc common.ServiceError // not an error type, so errors.AsType does not apply
	if errors.As(err, &svc) && svc.GetHTTPStatusCode() == http.StatusNotFound {
		return fmt.Errorf("%w: %w", secrets.ErrNotFound, err)
	}
	return err
}
