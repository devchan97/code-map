package walker

import "testing"

func TestIsSecretFile(t *testing.T) {
	secret := []string{
		".env", ".env.local", ".env.production",
		"id_rsa", "id_rsa.pub", "id_ed25519", "id_ecdsa", "id_dsa",
		"server.pem", "client.key", "cert.p12", "windows.pfx",
		"sub/dir/.env", "sub/.env.staging",
	}
	for _, p := range secret {
		if !IsSecretFile(p) {
			t.Errorf("IsSecretFile(%q) = false; want true", p)
		}
	}
	notSecret := []string{
		"main.go", "README.md", "test.py", "lib/utils.go", "envs.txt",
	}
	for _, p := range notSecret {
		if IsSecretFile(p) {
			t.Errorf("IsSecretFile(%q) = true; want false", p)
		}
	}
}

func TestContainsSecretPattern(t *testing.T) {
	if !ContainsSecretPattern([]byte("hello AKIAIOSFODNN7EXAMPLE world")) {
		t.Error("expected AWS key pattern detected")
	}
	if !ContainsSecretPattern([]byte("-----BEGIN RSA PRIVATE KEY-----\nblob")) {
		t.Error("expected PEM header detected")
	}
	if ContainsSecretPattern([]byte("just a normal source file")) {
		t.Error("false positive on normal text")
	}
	// AKIA prefix without 16 trailing alnum chars must NOT match.
	if ContainsSecretPattern([]byte("AKIA 12345")) {
		t.Error("AKIA without proper suffix should not match")
	}
}
