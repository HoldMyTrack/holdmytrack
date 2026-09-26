package httpapi

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Verifying a Google id_token a native client hands over — Sign in with Google on Android
// (docs/SPEC.md FR-1.9, docs/adr/0016-native-sign-in.md). The web flow's id_token comes
// straight from Google's token endpoint over TLS, so parseGoogleIDToken's claim checks are all
// it needs. One posted by an app could have been written by anyone, so its RS256 signature is
// checked against Google's published keys first — still with the standard library alone, no
// JWT library, as the rest of the Google flow.

const (
	googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"

	// googleKeysDefaultTTL applies when the certs response carries no usable max-age.
	googleKeysDefaultTTL = time.Hour
	// googleKeysRefetchGap bounds refetching on an unknown kid: Google publishes a new key
	// before signing with it, so an unknown kid normally means a stale cache — but a forged
	// token can name any kid, and each one mustn't cost a request to Google.
	googleKeysRefetchGap = time.Minute
)

// googleKeySet caches Google's id_token signing keys by kid, for as long as the certs
// response's Cache-Control allows.
type googleKeySet struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
}

func newGoogleKeySet(client *http.Client) *googleKeySet {
	return &googleKeySet{url: googleCertsURL, client: client}
}

// key returns the public key for kid, fetching the set when the cache has expired or doesn't
// have kid (at most once per googleKeysRefetchGap for the latter).
func (k *googleKeySet) key(ctx context.Context, kid string, now time.Time) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.keys[kid]; ok && now.Before(k.expires) {
		return key, nil
	}
	if k.keys != nil && now.Before(k.expires) && now.Sub(k.lastFetch) < googleKeysRefetchGap {
		return nil, fmt.Errorf("unknown signing key %q", kid)
	}
	if err := k.fetch(ctx, now); err != nil {
		return nil, err
	}
	if key, ok := k.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown signing key %q", kid)
}

func (k *googleKeySet) fetch(ctx context.Context, now time.Time) error {
	k.lastFetch = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return err
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("certs endpoint returned %d", resp.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return fmt.Errorf("certs response: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, jwk := range set.Keys {
		if jwk.Kty != "RSA" || jwk.Kid == "" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(jwk.N)
		e, errE := base64.RawURLEncoding.DecodeString(jwk.E)
		if errN != nil || errE != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		keys[jwk.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(keys) == 0 {
		return errors.New("certs response has no RSA keys")
	}
	k.keys = keys
	k.expires = now.Add(cacheMaxAge(resp.Header.Get("Cache-Control"), googleKeysDefaultTTL))
	return nil
}

// cacheMaxAge reads max-age out of a Cache-Control header, or returns fallback.
func cacheMaxAge(header string, fallback time.Duration) time.Duration {
	for _, directive := range strings.Split(header, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if !ok || !strings.EqualFold(name, "max-age") {
			continue
		}
		if secs, err := strconv.Atoi(value); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}

// verifyGoogleIDToken checks an id_token's RS256 signature against Google's keys, then its
// claims exactly as the web flow does (parseGoogleIDToken). The audience is the web client ID:
// Android's Credential Manager is asked for a token for that client (its serverClientId), which
// is what lets this server accept it.
func (s *Server) verifyGoogleIDToken(ctx context.Context, idToken string, now time.Time) (googleClaims, error) {
	segs := strings.Split(idToken, ".")
	if len(segs) != 3 {
		return googleClaims{}, errors.New("id_token is not a JWT")
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(segs[0])
	if err != nil {
		return googleClaims{}, fmt.Errorf("id_token header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return googleClaims{}, fmt.Errorf("id_token header: %w", err)
	}
	// Only RS256: accepting whatever the token names ("none", or HS256 keyed with a public
	// key) is the classic JWT forgery.
	if header.Alg != "RS256" {
		return googleClaims{}, fmt.Errorf("unexpected algorithm %q", header.Alg)
	}
	sig, err := base64.RawURLEncoding.DecodeString(segs[2])
	if err != nil {
		return googleClaims{}, fmt.Errorf("id_token signature: %w", err)
	}
	key, err := s.google.keys.key(ctx, header.Kid, now)
	if err != nil {
		return googleClaims{}, err
	}
	digest := sha256.Sum256([]byte(segs[0] + "." + segs[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return googleClaims{}, errors.New("id_token signature does not verify")
	}
	return parseGoogleIDToken(idToken, s.google.ClientID, now)
}
