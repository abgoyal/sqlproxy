// Package publicid provides encrypted public IDs to prevent PK enumeration,
// modification attacks, and cross-entity ID reuse.
package publicid

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
)

// Encoder handles encoding/decoding of public IDs using XTEA encryption
type Encoder struct {
	secret     string // Retained for future key rotation support (incoming/outgoing keys)
	namespaces map[string]*Namespace
}

// Namespace represents a configured namespace for public ID generation
type Namespace struct {
	Name   string
	Prefix string
	key    [4]uint32 // XTEA 128-bit key as 4 uint32s
}

// NamespaceConfig represents configuration for a namespace
type NamespaceConfig struct {
	Name   string `yaml:"name"`
	Prefix string `yaml:"prefix"`
}

// Base62Alphabet is the character set used for base62 encoding.
// Exported for use by template functions.
const Base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// NewEncoder creates encoder with derived keys per namespace
func NewEncoder(secret string, namespaces []NamespaceConfig) (*Encoder, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("secret key must be at least 32 characters")
	}

	e := &Encoder{
		secret:     secret,
		namespaces: make(map[string]*Namespace),
	}

	for _, ns := range namespaces {
		if ns.Name == "" {
			return nil, fmt.Errorf("namespace name cannot be empty")
		}
		key := deriveKey(secret, ns.Name)
		e.namespaces[ns.Name] = &Namespace{
			Name:   ns.Name,
			Prefix: ns.Prefix,
			key:    key,
		}
	}

	return e, nil
}

// deriveKey creates namespace-specific XTEA key using HMAC-SHA256
func deriveKey(secret, namespace string) [4]uint32 {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(namespace))
	hash := h.Sum(nil)[:16] // XTEA needs 128-bit key

	var key [4]uint32
	for i := 0; i < 4; i++ {
		key[i] = binary.BigEndian.Uint32(hash[i*4 : (i+1)*4])
	}
	return key
}

// Encode converts internal ID to public ID
func (e *Encoder) Encode(namespace string, id int64) (string, error) {
	ns, ok := e.namespaces[namespace]
	if !ok {
		return "", fmt.Errorf("unknown namespace: %s", namespace)
	}

	// XTEA encrypt the 64-bit ID
	encrypted := xteaEncrypt(uint64(id), ns.key)

	// Encode to base62
	encoded := encodeBase62(encrypted)

	// Add prefix if configured
	if ns.Prefix != "" {
		return ns.Prefix + "_" + encoded, nil
	}
	return encoded, nil
}

// Decode converts public ID back to internal ID
func (e *Encoder) Decode(namespace string, publicID string) (int64, error) {
	ns, ok := e.namespaces[namespace]
	if !ok {
		return 0, fmt.Errorf("unknown namespace: %s", namespace)
	}

	// Strip prefix if present
	encoded := publicID
	if ns.Prefix != "" {
		expectedPrefix := ns.Prefix + "_"
		if !strings.HasPrefix(publicID, expectedPrefix) {
			return 0, fmt.Errorf("invalid prefix for namespace %s", namespace)
		}
		encoded = strings.TrimPrefix(publicID, expectedPrefix)
	}

	// Decode from base62
	encrypted, err := decodeBase62(encoded)
	if err != nil {
		return 0, fmt.Errorf("invalid public ID format: %w", err)
	}

	// XTEA decrypt
	decrypted := xteaDecrypt(encrypted, ns.key)

	return int64(decrypted), nil
}

// HasNamespace checks if a namespace is configured
func (e *Encoder) HasNamespace(namespace string) bool {
	_, ok := e.namespaces[namespace]
	return ok
}

// XTEA encryption constants
const xteaDelta = 0x9E3779B9
const xteaRounds = 32

// xteaEncrypt encrypts a 64-bit value using XTEA
func xteaEncrypt(v uint64, key [4]uint32) uint64 {
	v0 := uint32(v >> 32)
	v1 := uint32(v)
	sum := uint32(0)

	for i := 0; i < xteaRounds; i++ {
		v0 += (((v1 << 4) ^ (v1 >> 5)) + v1) ^ (sum + key[sum&3])
		sum += xteaDelta
		v1 += (((v0 << 4) ^ (v0 >> 5)) + v0) ^ (sum + key[(sum>>11)&3])
	}

	return (uint64(v0) << 32) | uint64(v1)
}

// xteaDecrypt decrypts a 64-bit value using XTEA
func xteaDecrypt(v uint64, key [4]uint32) uint64 {
	v0 := uint32(v >> 32)
	v1 := uint32(v)
	// Calculate sum by letting it overflow naturally (same as encrypt)
	var sum uint32
	for i := 0; i < xteaRounds; i++ {
		sum += xteaDelta
	}

	for i := 0; i < xteaRounds; i++ {
		v1 -= (((v0 << 4) ^ (v0 >> 5)) + v0) ^ (sum + key[(sum>>11)&3])
		sum -= xteaDelta
		v0 -= (((v1 << 4) ^ (v1 >> 5)) + v1) ^ (sum + key[sum&3])
	}

	return (uint64(v0) << 32) | uint64(v1)
}

// encodeBase62 encodes a uint64 to base62 string
// Pads to 11 characters for consistent length
func encodeBase62(n uint64) string {
	if n == 0 {
		return "00000000000" // 11 zeros for consistent length
	}

	var result [11]byte
	for i := 10; i >= 0; i-- {
		result[i] = Base62Alphabet[n%62]
		n /= 62
	}

	return string(result[:])
}

// decodeBase62 decodes a base62 string to uint64
// Requires exactly 11 characters (our encoder always produces 11)
func decodeBase62(s string) (uint64, error) {
	// Our encoder always produces exactly 11 characters
	if len(s) != 11 {
		return 0, fmt.Errorf("invalid length: %d characters (expected 11)", len(s))
	}

	var result uint64
	for _, c := range s {
		idx := strings.IndexRune(Base62Alphabet, c)
		if idx < 0 {
			return 0, fmt.Errorf("invalid character: %c", c)
		}
		// Check for overflow before multiplication
		if result > (^uint64(0))/62 {
			return 0, fmt.Errorf("overflow during decode")
		}
		result *= 62
		// Check for overflow before addition
		if result > ^uint64(0)-uint64(idx) {
			return 0, fmt.Errorf("overflow during decode")
		}
		result += uint64(idx)
	}

	return result, nil
}
