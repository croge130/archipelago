// Tests in this file need a real PostgreSQL instance for Gatehouse-core's
// principal lookups; they skip without ARCHIPELAGO_TEST_DATABASE_URL.
// Ticket delivery is exercised over a real (not mocked) transit/inmem
// Session pair — proving Issue/Verify compose with an actual Transit
// message round-trip, not just with each other directly.
package sso

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	certstoreEvaluation "github.com/croge130/archipelago/certstore/evaluation"
	archidb "github.com/croge130/archipelago/db"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseDB "github.com/croge130/archipelago/gatehouse-core/storage/dbstore"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/sso/structure"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/wire"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func setupTest(t *testing.T) (*gatehouseDB.PostgresReader, *gatehouseDB.PostgresWriter) {
	t.Helper()
	dsn := os.Getenv("ARCHIPELAGO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ARCHIPELAGO_TEST_DATABASE_URL not set; skipping sso integration test")
	}

	pool, err := archidb.Open(context.Background(), archidb.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := gatehouseDB.Migrations()
	if err != nil {
		t.Fatalf("gatehouse Migrations: %v", err)
	}
	if err := pool.ProvisionSchemas(context.Background(), migrations); err != nil {
		t.Fatalf("gatehouse ProvisionSchemas: %v", err)
	}

	_, err = pool.Pgx().Exec(context.Background(), `TRUNCATE
		gatehouse_grants, gatehouse_role_permissions, gatehouse_group_memberships,
		gatehouse_groups, gatehouse_roles, gatehouse_sessions,
		gatehouse_password_credentials, gatehouse_token_credentials,
		gatehouse_totp_credentials, gatehouse_passkey_credentials,
		gatehouse_mtls_certificate_credentials, gatehouse_credentials,
		gatehouse_principals, gatehouse_permission_definitions,
		gatehouse_contexts, gatehouse_context_types, gatehouse_templates,
		gatehouse_authority_generation
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate gatehouse: %v", err)
	}

	return gatehouseDB.NewPostgresReader(pool.Pgx()), gatehouseDB.NewPostgresWriter(pool.Pgx())
}

// ticketSigner builds a self-signed ECDSA P-256 Signer standing in for
// the dedicated ticket-signing key — deliberately not part of any CA
// hierarchy, per 12-sso-tickets-model.md.
func ticketSigner(t *testing.T) certstoreEvaluation.Signer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sso test ticket signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certstoreEvaluation.NewSoftwareSigner(cert, key)
}

func TestIssueUnknownPrincipalDenied(t *testing.T) {
	ghReader, _ := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	signer := ticketSigner(t)
	if _, err := Issue(ctx, ghReader, signer, [16]byte{1}, "gamebridge", time.Minute); !errors.Is(err, ErrUnknownPrincipal) {
		t.Fatalf("expected ErrUnknownPrincipal, got: %v", err)
	}
}

func TestIssueAndVerifyEndToEndOverRealTransit(t *testing.T) {
	ghReader, ghWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "user.christian", gatehouseStructure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}

	signer := ticketSigner(t)
	ticket, err := Issue(ctx, ghReader, signer, p.PrincipalID, "gamebridge", time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Deliver over a real transit/inmem Session pair — the issuer's
	// side Pushes the ticket, the destination's side receives it via
	// Next, exactly the EventPush shape 12-sso-tickets-model.md's
	// Delivery section describes.
	issuerSide, destinationSide := inmem.NewPipe()
	defer issuerSide.Close()
	defer destinationSide.Close()

	payload, err := json.Marshal(ticket)
	if err != nil {
		t.Fatalf("marshal ticket: %v", err)
	}
	if err := issuerSide.Push(ctx, wire.Message{Type: "sso.ticket", Kind: wire.KindEvent, Payload: payload}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	received, err := destinationSide.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if received.Type != "sso.ticket" {
		t.Fatalf("Type = %q, want sso.ticket", received.Type)
	}
	var deliveredTicket structure.Ticket
	if err := json.Unmarshal(received.Payload, &deliveredTicket); err != nil {
		t.Fatalf("unmarshal delivered ticket: %v", err)
	}

	principal, err := Verify(ctx, ghReader, signer.Certificate(), deliveredTicket, "gamebridge", time.Now())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if principal.PrincipalID != p.PrincipalID {
		t.Fatalf("Verify resolved %s, want %s", principal.PrincipalID, p.PrincipalID)
	}
}

func TestVerifyAudienceMismatch(t *testing.T) {
	ghReader, ghWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "user.christian", gatehouseStructure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	signer := ticketSigner(t)
	ticket, err := Issue(ctx, ghReader, signer, p.PrincipalID, "gamebridge", time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := Verify(ctx, ghReader, signer.Certificate(), ticket, "storage-manager", time.Now()); !errors.Is(err, ErrAudienceMismatch) {
		t.Fatalf("expected ErrAudienceMismatch, got: %v", err)
	}
}

func TestVerifyExpiredTicket(t *testing.T) {
	ghReader, ghWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "user.christian", gatehouseStructure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	signer := ticketSigner(t)
	ticket, err := Issue(ctx, ghReader, signer, p.PrincipalID, "gamebridge", time.Millisecond)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := Verify(ctx, ghReader, signer.Certificate(), ticket, "gamebridge", time.Now().Add(time.Hour)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got: %v", err)
	}
}

func TestVerifyWrongSigningKeyDenied(t *testing.T) {
	ghReader, ghWriter := setupTest(t)
	ctx, cancel := withTimeout(t)
	defer cancel()

	p, err := gatehouseFacade.EnsurePrincipal(ctx, ghReader, ghWriter, "user.christian", gatehouseStructure.PrincipalTypeUser)
	if err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	realSigner := ticketSigner(t)
	ticket, err := Issue(ctx, ghReader, realSigner, p.PrincipalID, "gamebridge", time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	impostor := ticketSigner(t) // a different key entirely
	if _, err := Verify(ctx, ghReader, impostor.Certificate(), ticket, "gamebridge", time.Now()); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got: %v", err)
	}
}
