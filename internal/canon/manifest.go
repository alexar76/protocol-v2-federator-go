package canon

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ManifestCanonical builds the §7.3.2 five-field string.
func ManifestCanonical(capabilitiesCount int, generatedAt, protocolVersion string, tools any, byHub any) (string, error) {
	if protocolVersion == "" {
		protocolVersion = "v1"
	}
	if byHub == nil {
		byHub = map[string]any{}
	}
	toolsHash, err := DigestPython(tools)
	if err != nil {
		return "", err
	}
	byHubHash, err := DigestPython(byHub)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"capabilities_count:%d|generated_at:%s|protocol_version:%s|tools_hash:%s|by_hub_hash:%s",
		capabilitiesCount, generatedAt, protocolVersion, toolsHash, byHubHash,
	), nil
}

// DigestPython is sha256(python-json) as lowercase hex (§7.3.1 / §7.3.2).
func DigestPython(v any) (string, error) {
	b, err := MarshalPython(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:]), nil
}

// VerifyManifest checks the signature with the pinned well-known public key.
// If signature.public_key is present and differs from the pin, the manifest is rejected.
func VerifyManifest(doc map[string]any, pinnedPubKeyB64 string) error {
	sigObj, ok := doc["signature"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing signature object")
	}
	alg, _ := sigObj["algorithm"].(string)
	if alg != "ed25519" {
		return fmt.Errorf("unsupported algorithm %q", alg)
	}
	if inner, ok := sigObj["public_key"].(string); ok && inner != "" && inner != pinnedPubKeyB64 {
		return fmt.Errorf("signature.public_key does not match pinned signer_public_key")
	}
	valB64, _ := sigObj["value"].(string)
	sig, err := base64.StdEncoding.DecodeString(valB64)
	if err != nil {
		return fmt.Errorf("signature value: %w", err)
	}
	pub, err := base64.StdEncoding.DecodeString(pinnedPubKeyB64)
	if err != nil {
		return fmt.Errorf("public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key length %d", len(pub))
	}

	count := 0
	switch c := doc["capabilities_count"].(type) {
	case json.Number:
		n, err := c.Int64()
		if err != nil {
			return err
		}
		count = int(n)
	case float64:
		count = int(c)
	case int:
		count = c
	}

	generatedAt, _ := doc["generated_at"].(string)
	protocolVersion, _ := doc["protocol_version"].(string)
	if protocolVersion == "" {
		protocolVersion = "v1"
	}
	tools := doc["tools"]
	if tools == nil {
		tools = []any{}
	}
	byHub := doc["by_hub"]
	if byHub == nil {
		byHub = map[string]any{}
	}

	canon, err := ManifestCanonical(count, generatedAt, protocolVersion, tools, byHub)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(canon), sig) {
		return fmt.Errorf("signature invalid")
	}
	return nil
}

// SignManifest returns the signature object for a §7.3.2 canonical string.
func SignManifest(private ed25519.PrivateKey, publicB64, canonical string) map[string]any {
	sig := ed25519.Sign(private, []byte(canonical))
	return map[string]any{
		"algorithm":  "ed25519",
		"public_key": publicB64,
		"value":      base64.StdEncoding.EncodeToString(sig),
	}
}
