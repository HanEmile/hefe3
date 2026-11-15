package federation

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	headerOrigin    = "X-Xilebo-Origin"
	headerTimestamp = "X-Xilebo-Timestamp"
	headerSignature = "X-Xilebo-Signature"

	// Maximum clock skew allowed (5 minutes)
	maxClockSkew = 5 * time.Minute
)

// SignRequest signs an outbound federation request.
// Signature covers: METHOD\nPATH\nTIMESTAMP\nBODY_HASH
func SignRequest(req *http.Request, origin string, privateKey ed25519.PrivateKey, body []byte) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s",
		req.Method,
		req.URL.Path,
		timestamp,
		base64.RawStdEncoding.EncodeToString(bodyHash[:]),
	)

	signature := ed25519.Sign(privateKey, []byte(canonical))

	req.Header.Set(headerOrigin, origin)
	req.Header.Set(headerTimestamp, timestamp)
	req.Header.Set(headerSignature, base64.RawStdEncoding.EncodeToString(signature))
}

// VerifyRequest verifies the signature on an inbound federation request.
func VerifyRequest(r *http.Request, publicKey ed25519.PublicKey, body []byte) error {
	origin := r.Header.Get(headerOrigin)
	if origin == "" {
		return fmt.Errorf("missing %s header", headerOrigin)
	}

	timestampStr := r.Header.Get(headerTimestamp)
	if timestampStr == "" {
		return fmt.Errorf("missing %s header", headerTimestamp)
	}

	sigStr := r.Header.Get(headerSignature)
	if sigStr == "" {
		return fmt.Errorf("missing %s header", headerSignature)
	}

	// Check timestamp freshness
	tsMillis, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	ts := time.UnixMilli(tsMillis)
	if time.Since(ts).Abs() > maxClockSkew {
		return fmt.Errorf("timestamp too far from current time (skew: %s)", time.Since(ts))
	}

	// Reconstruct canonical representation
	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s",
		r.Method,
		r.URL.Path,
		timestampStr,
		base64.RawStdEncoding.EncodeToString(bodyHash[:]),
	)

	// Verify signature
	signature, err := base64.RawStdEncoding.DecodeString(sigStr)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}

	if !ed25519.Verify(publicKey, []byte(canonical), signature) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// OriginFromRequest extracts the origin server name from a federation request.
func OriginFromRequest(r *http.Request) string {
	return r.Header.Get(headerOrigin)
}
