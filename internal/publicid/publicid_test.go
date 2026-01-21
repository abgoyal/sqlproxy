package publicid

import (
	"testing"
)

func TestNewEncoder(t *testing.T) {
	tests := []struct {
		name       string
		secret     string
		namespaces []NamespaceConfig
		wantErr    bool
	}{
		{
			name:   "valid configuration",
			secret: "this-is-a-secret-key-that-is-32chars",
			namespaces: []NamespaceConfig{
				{Name: "user", Prefix: "usr"},
				{Name: "order", Prefix: "ord"},
			},
			wantErr: false,
		},
		{
			name:       "secret too short",
			secret:     "short",
			namespaces: []NamespaceConfig{{Name: "user"}},
			wantErr:    true,
		},
		{
			name:       "empty namespace name",
			secret:     "this-is-a-secret-key-that-is-32chars",
			namespaces: []NamespaceConfig{{Name: "", Prefix: "usr"}},
			wantErr:    true,
		},
		{
			name:       "no prefix (valid)",
			secret:     "this-is-a-secret-key-that-is-32chars",
			namespaces: []NamespaceConfig{{Name: "user"}},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEncoder(tt.secret, tt.namespaces)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEncoder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEncodeDecode(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{
		{Name: "user", Prefix: "usr"},
		{Name: "order", Prefix: "ord"},
		{Name: "noprefix"},
	})
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}

	tests := []struct {
		name      string
		namespace string
		id        int64
	}{
		{"user id 1", "user", 1},
		{"user id 2", "user", 2},
		{"user id 100", "user", 100},
		{"user id max", "user", 9223372036854775807},
		{"user id 0", "user", 0},
		{"user id -1", "user", -1},
		{"user id -100", "user", -100},
		{"user id min", "user", -9223372036854775808},
		{"order id 1", "order", 1},
		{"noprefix id 42", "noprefix", 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := enc.Encode(tt.namespace, tt.id)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			decoded, err := enc.Decode(tt.namespace, encoded)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if decoded != tt.id {
				t.Errorf("Round-trip failed: got %d, want %d", decoded, tt.id)
			}
		})
	}
}

func TestDifferentIDsProduceDifferentOutput(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	encoded1, err := enc.Encode("user", 1)
	if err != nil {
		t.Fatal(err)
	}
	encoded2, err := enc.Encode("user", 2)
	if err != nil {
		t.Fatal(err)
	}

	if encoded1 == encoded2 {
		t.Error("Different IDs should produce different encoded values")
	}
}

func TestSameIDDifferentNamespaceProducesDifferentOutput(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{
		{Name: "user", Prefix: "usr"},
		{Name: "order", Prefix: "ord"},
	})
	if err != nil {
		t.Fatal(err)
	}

	userEncoded, err := enc.Encode("user", 1)
	if err != nil {
		t.Fatal(err)
	}
	orderEncoded, err := enc.Encode("order", 1)
	if err != nil {
		t.Fatal(err)
	}

	if userEncoded == orderEncoded {
		t.Error("Same ID in different namespaces should produce different encoded values")
	}
}

func TestPrefixHandling(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{
		{Name: "user", Prefix: "usr"},
		{Name: "noprefix"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// With prefix
	encoded, err := enc.Encode("user", 42)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[:4] != "usr_" {
		t.Errorf("Expected prefix 'usr_', got %s", encoded[:4])
	}

	// Without prefix
	encoded, err = enc.Encode("noprefix", 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 11 {
		t.Errorf("Expected 11 chars without prefix, got %d", len(encoded))
	}
}

func TestDecodeInvalidPrefix(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = enc.Decode("user", "ord_00000000001")
	if err == nil {
		t.Error("Expected error for invalid prefix")
	}
}

func TestDecodeUnknownNamespace(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = enc.Decode("unknown", "usr_00000000001")
	if err == nil {
		t.Error("Expected error for unknown namespace")
	}
}

func TestEncodeUnknownNamespace(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = enc.Encode("unknown", 1)
	if err == nil {
		t.Error("Expected error for unknown namespace")
	}
}

func TestDecodeInvalidCharacters(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "noprefix"}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = enc.Decode("noprefix", "!!!invalid!!!")
	if err == nil {
		t.Error("Expected error for invalid characters")
	}
}

func TestDecodeInvalidLength(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "test", Prefix: "tst"}})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		input string
	}{
		{"empty", "tst_"},
		{"too short", "tst_abc"},
		{"too long", "tst_abcdefghijkl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decode("test", tt.input)
			if err == nil {
				t.Errorf("Expected error for %s input %q", tt.name, tt.input)
			}
		})
	}
}

func TestHasNamespace(t *testing.T) {
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	if !enc.HasNamespace("user") {
		t.Error("Expected HasNamespace to return true for 'user'")
	}
	if enc.HasNamespace("unknown") {
		t.Error("Expected HasNamespace to return false for 'unknown'")
	}
}

func TestXTEARoundTrip(t *testing.T) {
	// Test vectors using arbitrary but distinct values covering different bit patterns
	key := [4]uint32{0x12345678, 0x9ABCDEF0, 0x13579BDF, 0x2468ACE0}

	tests := []uint64{
		0,
		1,
		0xFFFFFFFFFFFFFFFF,
		0x123456789ABCDEF0,
	}

	for _, v := range tests {
		encrypted := xteaEncrypt(v, key)
		decrypted := xteaDecrypt(encrypted, key)
		if decrypted != v {
			t.Errorf("XTEA round-trip failed: got %x, want %x", decrypted, v)
		}
	}
}

func TestBase62RoundTrip(t *testing.T) {
	tests := []uint64{
		0,
		1,
		62,
		3843, // 62^2 - 1
		0xFFFFFFFFFFFFFFFF,
	}

	for _, v := range tests {
		encoded := encodeBase62(v)
		decoded, err := decodeBase62(encoded)
		if err != nil {
			t.Errorf("decodeBase62 failed: %v", err)
			continue
		}
		if decoded != v {
			t.Errorf("Base62 round-trip failed: got %d, want %d", decoded, v)
		}
	}
}

func TestConsistentEncoding(t *testing.T) {
	// Encoding should be deterministic - same input produces same output
	secret := "this-is-a-secret-key-that-is-32chars"
	enc, err := NewEncoder(secret, []NamespaceConfig{{Name: "user", Prefix: "usr"}})
	if err != nil {
		t.Fatal(err)
	}

	encoded1, err := enc.Encode("user", 12345)
	if err != nil {
		t.Fatal(err)
	}
	encoded2, err := enc.Encode("user", 12345)
	if err != nil {
		t.Fatal(err)
	}

	if encoded1 != encoded2 {
		t.Error("Same input should produce same encoded output")
	}
}

func TestDifferentSecretsProduceDifferentOutput(t *testing.T) {
	enc1, err := NewEncoder("secret-key-number-one-is-32-chars!", []NamespaceConfig{{Name: "user"}})
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := NewEncoder("secret-key-number-two-is-32-chars!", []NamespaceConfig{{Name: "user"}})
	if err != nil {
		t.Fatal(err)
	}

	encoded1, err := enc1.Encode("user", 42)
	if err != nil {
		t.Fatal(err)
	}
	encoded2, err := enc2.Encode("user", 42)
	if err != nil {
		t.Fatal(err)
	}

	if encoded1 == encoded2 {
		t.Error("Different secrets should produce different encoded values")
	}
}
