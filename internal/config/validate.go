package config

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var providerIDPattern = regexp.MustCompile(`^[a-z0-9]+$`)

// reservedProviderIDs are the prefixes the Anthropic passthrough answers to.
var reservedProviderIDs = []string{"anthropic", "claude"}

// headerNameSpecials are the non-alphanumeric characters an HTTP field name
// may use, from RFC 9110 token.
const headerNameSpecials = "!#$%&'*+-.^_`|~"

// Validate reports every problem with the config at once.
func (c *Config) Validate() error {
	errs := []error{
		validateListen(c.Listen),
		validateBaseURL("anthropic.base_url", c.Anthropic.BaseURL),
	}

	idOwner := make(map[string]string, len(c.Providers))
	for i, p := range c.Providers {
		errs = append(errs, p.validate(fmt.Sprintf("providers[%d]", i), idOwner)...)
	}

	return errors.Join(errs...)
}

func (p Provider) validate(field string, idOwner map[string]string) []error {
	errs := []error{
		p.validateID(field, idOwner),
		validateBaseURL(field+".base_url", p.BaseURL),
	}
	if p.APIKey == "" {
		errs = append(errs, fmt.Errorf("%s.api_key is required", field))
	}
	errs = append(errs, validateHeaders(field+".headers", p.Headers)...)

	return errs
}

func (p Provider) validateID(field string, idOwner map[string]string) error {
	switch {
	case p.ID == "":
		return fmt.Errorf("%s.id is required", field)
	case strings.Contains(p.ID, "-"):
		return fmt.Errorf(`%s.id %q must not contain "-", because the router splits a model id on the first "-" to find the provider`, field, p.ID)
	case !providerIDPattern.MatchString(p.ID):
		return fmt.Errorf("%s.id %q must match %s, so only lower case letters and digits", field, p.ID, providerIDPattern)
	case slices.Contains(reservedProviderIDs, p.ID):
		return fmt.Errorf("%s.id %q is reserved for the Anthropic passthrough", field, p.ID)
	}

	if owner, taken := idOwner[p.ID]; taken {
		return fmt.Errorf("%s.id %q is already used by %s", field, p.ID, owner)
	}
	idOwner[p.ID] = field

	return nil
}

func validateListen(listen string) error {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen %q must be a host:port address: %w", listen, err)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("listen %q must use a numeric port between 0 and 65535, got %q", listen, port)
	}
	return nil
}

func validateBaseURL(field, rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s %q must be a valid URL: %w", field, rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s %q must be an absolute URL using http or https", field, rawURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s %q must be an absolute URL with a host", field, rawURL)
	}
	return nil
}

func validateHeaders(field string, headers map[string]string) []error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		if !isHeaderName(name) {
			errs = append(errs, fmt.Errorf("%s has the invalid header name %q, which must only use letters, digits and %s", field, name, headerNameSpecials))
		}
		if strings.ContainsAny(headers[name], "\r\n") {
			errs = append(errs, fmt.Errorf("%s value of %q must not contain a line break", field, name))
		}
	}
	return errs
}

func isHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alphanumeric && !strings.ContainsRune(headerNameSpecials, r) {
			return false
		}
	}
	return true
}
