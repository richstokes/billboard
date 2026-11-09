package main

import (
	"strings"
	"testing"
)

func TestIsSecretFile(t *testing.T) {
	secret := []string{
		"prod.env", "app.env.local", "server.pem", "tls.key", "cert.p12", "id_rsa", "id_ed25519.pub",
		"terraform.tfstate", "terraform.tfstate.backup", "prod.tfvars", "secrets.yaml",
		"service-account.json", "credentials", "client_secret.json", "db-password.txt", "vault.kdbx",
	}
	fine := []string{
		"main.go", "env.go", "environment.ts", "envelope.py", "secrets.go", "token_test.go",
		"keyboard.c", "config.yaml", "package.json", "Makefile", "README.md",
	}
	for _, n := range secret {
		if !isSecretFile(n) {
			t.Errorf("%s should be excluded", n)
		}
	}
	for _, n := range fine {
		if isSecretFile(n) {
			t.Errorf("%s should not be excluded", n)
		}
	}
}

func TestHasSecret(t *testing.T) {
	r := func(n int) string { return strings.Repeat("a1B2", n)[:n] }
	secret := []string{
		"-----BEGIN RSA " + "PRIVATE KEY-----\nabc",
		`key := "AKIA` + strings.ToUpper(r(16)) + `"`,
		"token = ghp_" + r(36),
		"const k = 'sk-ant-" + r(40) + "'",
		`"type": "service_account",`,
		`DATABASE_URL = "postgres://admin:` + r(12) + `@db.internal:5432/app"`,
		`password: "` + r(16) + `"`,
		`apiKey = "` + r(24) + `"`,
	}
	fine := []string{
		"func main() {\n\tfmt.Println(\"hi\")\n}",
		`password := os.Getenv("DB_PASSWORD")`,
		`apiKey := cfg.APIKey`,
		`password: "changeme"`,
		`url := "https://example.com/path"`,
		`// read the token from the keychain`,
	}
	for _, s := range secret {
		if !hasSecret(s) {
			t.Errorf("should flag: %q", s)
		}
	}
	for _, s := range fine {
		if hasSecret(s) {
			t.Errorf("should not flag: %q", s)
		}
	}
}
