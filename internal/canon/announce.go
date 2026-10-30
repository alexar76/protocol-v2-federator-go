package canon

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// AnnounceCanonical builds the §7.3.5 three-field string.
func AnnounceCanonical(hubURL, wellKnownURL string, capabilitiesCount int) string {
	return fmt.Sprintf(
		"hub_url:%s|well_known_url:%s|capabilities_count:%d",
		hubURL, wellKnownURL, capabilitiesCount,
	)
}

// VerifyAnnounce checks the announce signature with signer_public_key from the body.
func VerifyAnnounce(doc map[string]any) error {
	hubURL, _ := doc["hub_url"].(string)
	wellKnownURL, _ := doc["well_known_url"].(string)
	if hubURL == "" || wellKnownURL == "" {
		return fmt.Errorf("missing hub_url or well_known_url")
	}
	count := 0
	switch c := doc["capabilities_count"].(type) {
	case nil:
		count = 0
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

	pubB64, _ := doc["signer_public_key"].(string)
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return fmt.Errorf("signer_public_key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("signer_public_key length %d", len(pub))
	}

	sigObj, ok := doc["signature"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing signature object")
	}
	if alg, _ := sigObj["algorithm"].(string); alg != "ed25519" {
		return fmt.Errorf("unsupported algorithm %q", alg)
	}
	valB64, _ := sigObj["value"].(string)
	sig, err := base64.StdEncoding.DecodeString(valB64)
	if err != nil {
		return fmt.Errorf("signature value: %w", err)
	}

	canon := AnnounceCanonical(hubURL, wellKnownURL, count)
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(canon), sig) {
		return fmt.Errorf("signature invalid")
	}
	return nil
}

// SignAnnounce signs a §7.3.5 canonical with the operator key.
func SignAnnounce(private ed25519.PrivateKey, hubURL, wellKnownURL string, capabilitiesCount int) (string, error) {
	canon := AnnounceCanonical(hubURL, wellKnownURL, capabilitiesCount)
	sig := ed25519.Sign(private, []byte(canon))
	return base64.StdEncoding.EncodeToString(sig), nil
}
