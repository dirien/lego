package civo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/civo/civogo"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/platform/config/env"
)

const (
	minTTL                    = 600
	defaultPollingInterval    = 30 * time.Second
	defaultPropagationTimeout = 300 * time.Second
)

// Environment variables names.
const (
	envNamespace = "CIVO_"

	EnvAPIToken = envNamespace + "TOKEN"

	EnvTTL                = envNamespace + "TTL"
	EnvPropagationTimeout = envNamespace + "PROPAGATION_TIMEOUT"
	EnvPollingInterval    = envNamespace + "POLLING_INTERVAL"
)

// Config is used to configure the creation of the DNSProvider.
type Config struct {
	ProjectID          string
	Token              string
	PropagationTimeout time.Duration
	PollingInterval    time.Duration
	TTL                int
}

// NewDefaultConfig returns a default configuration for the DNSProvider.
func NewDefaultConfig() *Config {
	return &Config{
		TTL:                env.GetOrDefaultInt(EnvTTL, minTTL),
		PropagationTimeout: env.GetOrDefaultSecond(EnvPropagationTimeout, defaultPropagationTimeout),
		PollingInterval:    env.GetOrDefaultSecond(EnvPollingInterval, defaultPollingInterval),
	}
}

// DNSProvider implements the challenge.Provider interface.
type DNSProvider struct {
	config *Config
	client *civogo.Client
}

// NewDNSProvider returns a DNSProvider instance configured for Scaleway Domains API.
// Credentials must be passed in the environment variables:
// API_TOKEN.
func NewDNSProvider() (*DNSProvider, error) {
	values, err := env.Get(EnvAPIToken)
	if err != nil {
		return nil, fmt.Errorf("civo: %w", err)
	}

	config := NewDefaultConfig()
	config.Token = values[EnvAPIToken]

	return NewDNSProviderConfig(config)
}

// NewDNSProviderConfig return a DNSProvider instance configured for scaleway.
func NewDNSProviderConfig(config *Config) (*DNSProvider, error) {
	if config == nil {
		return nil, errors.New("civo: the configuration of the DNS provider is nil")
	}

	if config.Token == "" {
		return nil, errors.New("civo: credentials missing")
	}

	if config.TTL < minTTL {
		config.TTL = minTTL
	}

	// Create a Civo client - DNS is region independent, we can use any region
	client, err := civogo.NewClient(config.Token, "LON1")
	if err != nil {
		return nil, fmt.Errorf("civo: %w", err)
	}

	return &DNSProvider{config: config, client: client}, nil
}

func (d *DNSProvider) Present(domain, token, keyAuth string) error {
	fqdn, value := dns01.GetRecord(domain, keyAuth)

	zone, err := getZone(fqdn)
	if err != nil {
		return fmt.Errorf("civo: failed to find zone: fqdn=%s: %w", fqdn, err)
	}
	dnsDomain, err := d.client.GetDNSDomain(zone)
	if err != nil {
		return fmt.Errorf("civo: %w", err)
	}
	_, err = d.client.CreateDNSRecord(dnsDomain.ID, &civogo.DNSRecordConfig{
		Name:  extractRecordName(fqdn, zone),
		Value: value,
		Type:  civogo.DNSRecordTypeTXT,
		TTL:   d.config.TTL,
	})
	if err != nil {
		return fmt.Errorf("civo: %w", err)
	}

	return nil
}

func (d *DNSProvider) CleanUp(domain, token, keyAuth string) error {
	fqdn, value := dns01.GetRecord(domain, keyAuth)
	zone, err := getZone(fqdn)
	if err != nil {
		return fmt.Errorf("civo: failed to find zone: fqdn=%s: %w", fqdn, err)
	}
	dnsDomain, err := d.client.GetDNSDomain(zone)
	if err != nil {
		return fmt.Errorf("civo: %w", err)
	}
	dnsRecords, err := d.client.ListDNSRecords(dnsDomain.ID)
	if err != nil {
		return fmt.Errorf("civo: %w", err)
	}

	var dnsRecord civogo.DNSRecord
	for _, entry := range dnsRecords {
		if entry.Name == extractRecordName(fqdn, zone) && entry.Value == value {
			dnsRecord = entry
			break
		}
	}

	_, err = d.client.DeleteDNSRecord(&dnsRecord)
	if err != nil {
		return fmt.Errorf("civo: %w", err)
	}
	return nil
}

func (d *DNSProvider) Timeout() (timeout, interval time.Duration) {
	return d.config.PropagationTimeout, d.config.PollingInterval
}

func getZone(fqdn string) (string, error) {
	authZone, err := dns01.FindZoneByFqdn(fqdn)
	if err != nil {
		return "", err
	}

	return dns01.UnFqdn(authZone), nil
}

func extractRecordName(fqdn, zone string) string {
	name := dns01.UnFqdn(fqdn)
	if idx := strings.Index(name, "."+zone); idx != -1 {
		return name[:idx]
	}
	return name
}
