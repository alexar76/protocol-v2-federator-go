package key

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

// Generate writes a 64-byte Ed25519 private key to path (mode 0600)
// and returns the public key as standard base64 with padding.
func Generate(path string) (pubB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, priv, 0o600); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// Load reads a 64-byte private key file.
func Load(path string) (ed25519.PrivateKey, ed25519.PublicKey, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, nil, "", fmt.Errorf("key file: want %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	priv := ed25519.PrivateKey(b)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, base64.StdEncoding.EncodeToString(pub), nil
}
