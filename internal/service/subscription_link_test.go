package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jiuwusan/z-jellyfin-hub/internal/secret"
)

func TestSavedSubscriptionLinkAndLegacyRestore(t *testing.T) {
	s, sub, link := fixture(t)
	token := strings.Split(link, "#")[1]
	got, err := s.SubscriptionLink(sub.ID)
	if err != nil || got != link {
		t.Fatal("saved link not recoverable")
	}
	row, _ := s.Store.Subscription(sub.ID)
	if !row.HasSavedToken || len(row.TokenCipher) == 0 || strings.Contains(string(row.TokenCipher), token) {
		t.Fatal("missing encrypted token")
	}
	raw, _ := json.Marshal(row)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "TokenCipher") || strings.Contains(string(raw), "TokenHash") {
		t.Fatal("ordinary subscription response exposes secrets")
	}
	if strings.Contains(string(raw), base64.StdEncoding.EncodeToString(row.TokenCipher)) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(row.TokenHash)) {
		t.Fatal("ordinary subscription response exposes stored token material")
	}
	row.Label = "Renamed"
	if _, _, err = s.SaveSubscription(row); err != nil {
		t.Fatal(err)
	}
	if got, err = s.SubscriptionLink(sub.ID); err != nil || got != link {
		t.Fatal("draft edit changed saved link")
	}
	preview, err := s.Preview(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(sub.ID, preview.ExpectedRevision, preview.DraftDigest, "fixture"); err != nil {
		t.Fatal(err)
	}
	before, err := s.Current(sub.ID, token, 57)
	if err != nil {
		t.Fatal(err)
	}
	row, _ = s.Store.Subscription(sub.ID)
	digest, _ := secret.Digest(token)
	if err = s.Store.SetToken(sub.ID, digest); err != nil {
		t.Fatal(err)
	} // Simulate a hash-only legacy record.
	if _, err = s.SubscriptionLink(sub.ID); !errors.Is(err, ErrTokenUnavailable) {
		t.Fatal("legacy token was fabricated")
	}
	wrong, _ := secret.Token()
	if _, err = s.RestoreSubscriptionLink(sub.ID, wrong); !errors.Is(err, ErrTokenMismatch) {
		t.Fatal("mismatched old token accepted")
	}
	if _, err = s.AuthenticateToken(token); err != nil {
		t.Fatal("failed restore invalidated existing devices")
	}
	if got, err = s.RestoreSubscriptionLink(sub.ID, token); err != nil || got != link {
		t.Fatal("old link was not restored")
	}
	after, _ := s.Store.Subscription(sub.ID)
	if after.Revision != row.Revision || !after.HasSavedToken {
		t.Fatal("restore changed revision or failed to persist")
	}
	current, err := s.Current(sub.ID, token, 57)
	if err != nil || fingerprint(current) != fingerprint(before) {
		t.Fatal("restore changed the published device configuration")
	}
	rotated, err := s.Rotate(sub.ID)
	if err != nil || rotated == link {
		t.Fatal("rotation failed")
	}
	if got, err = s.SubscriptionLink(sub.ID); err != nil || got != rotated {
		t.Fatal("copy retained obsolete token")
	}
	if _, err = s.AuthenticateToken(token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("rotated token still authenticates")
	}
}
