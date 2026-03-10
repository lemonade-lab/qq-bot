package forward

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// DeriveKeyPair derives an Ed25519 key pair from a QQ Bot secret.
// The secret is used as the seed (padded/truncated to 32 bytes).
func DeriveKeyPair(secret string) (ed25519.PrivateKey, ed25519.PublicKey) {
	seed := make([]byte, ed25519.SeedSize)
	copy(seed, []byte(secret))

	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)

	return privKey, pubKey
}

// VerifySignature verifies a QQ Bot webhook Ed25519 signature.
func VerifySignature(pubKey ed25519.PublicKey, timestamp, body, hexSig string) bool {
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}

	msg := []byte(timestamp + body)

	return ed25519.Verify(pubKey, msg, sig)
}

// SignChallenge creates a signature for the webhook validation challenge (op=13).
func SignChallenge(privKey ed25519.PrivateKey, eventTs, plainToken string) string {
	msg := []byte(eventTs + plainToken)
	sig := ed25519.Sign(privKey, msg)

	return fmt.Sprintf("%x", sig)
}
