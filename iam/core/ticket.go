package core

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const tenantTicketTTL = 5 * time.Minute

// ticketClaims are the verified contents of a tenant ticket.
type ticketClaims struct {
	jti string
	exp time.Time
}

// signTenantTicket binds a successful authentication to userID and the login
// flow (provider) for SelectTenant. The random jti lets a TicketStore make it single-use.
func signTenantTicket(key []byte, provider, userID string, now time.Time) string {
	exp := strconv.FormatInt(now.Add(tenantTicketTTL).Unix(), 10)
	jti := rand.Text()
	payload := base64.RawURLEncoding.EncodeToString([]byte(exp + "|" + jti + "|" + provider + "|" + userID))
	return payload + "." + ticketMAC(key, payload)
}

func verifyTenantTicket(key []byte, ticket, provider, userID string, now time.Time) (ticketClaims, error) {
	payload, mac, ok := strings.Cut(ticket, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(ticketMAC(key, payload))) {
		return ticketClaims{}, ErrInvalidTenantTicket
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ticketClaims{}, ErrInvalidTenantTicket
	}
	// userID is last so it may contain the separator.
	parts := strings.SplitN(string(raw), "|", 4)
	if len(parts) != 4 || parts[1] == "" {
		return ticketClaims{}, ErrInvalidTenantTicket
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || parts[2] != provider || parts[3] != userID || now.Unix() > exp {
		return ticketClaims{}, ErrInvalidTenantTicket
	}
	return ticketClaims{jti: parts[1], exp: time.Unix(exp, 0)}, nil
}

func ticketMAC(key []byte, payload string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("gofi/iam tenant-ticket/v3:" + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// issueTenantTicket returns "" when no ticket key is configured; SelectTenant then fails closed.
func (c AuthConfig) issueTenantTicket(provider, userID string) string {
	if len(c.ticketKey) == 0 {
		return ""
	}
	return signTenantTicket(c.ticketKey, provider, userID, time.Now())
}

// checkTenantTicket fails closed: without a key or a valid ticket SelectTenant is refused.
func (c AuthConfig) checkTenantTicket(ticket, provider, userID string) (ticketClaims, error) {
	if c.skipTicket {
		return ticketClaims{}, nil
	}
	if len(c.ticketKey) == 0 || ticket == "" {
		return ticketClaims{}, ErrInvalidTenantTicket
	}
	return verifyTenantTicket(c.ticketKey, ticket, provider, userID, time.Now())
}

// consumeTicket makes the ticket single-use when a TicketStore is configured.
// Without a store a ticket stays reusable until it expires (5 minutes).
func (c AuthConfig) consumeTicket(ctx context.Context, t ticketClaims) error {
	if c.tickets == nil || t.jti == "" {
		return nil
	}
	// Kept a little past exp so clock skew between instances cannot reopen it.
	first, err := c.tickets.Consume(ctx, t.jti, time.Until(t.exp)+time.Minute)
	if err != nil {
		return fmt.Errorf("iam: consume tenant ticket: %w", err)
	}
	if !first {
		return ErrInvalidTenantTicket
	}
	return nil
}
