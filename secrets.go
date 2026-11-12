package main

import (
	"regexp"
	"strings"
)

// This thing gets shown on livestreams, so anything that might hold a secret is
// excluded outright. It can't be opted back in with --ext or the picker.

var secretExts = map[string]bool{
	".env": true, ".pem": true, ".key": true, ".p12": true, ".pfx": true, ".p8": true,
	".jks": true, ".keystore": true, ".der": true, ".crt": true, ".cer": true, ".csr": true,
	".asc": true, ".gpg": true, ".pgp": true, ".age": true, ".kdbx": true, ".ovpn": true,
	".tfvars": true, ".tfstate": true, ".ppk": true, ".keytab": true, ".htpasswd": true,
	".netrc": true, ".npmrc": true, ".pypirc": true, ".secret": true, ".secrets": true,
	".credentials": true, ".pkcs12": true, ".jwk": true,
}

var secretNames = map[string]bool{
	"credentials": true, "kubeconfig": true, "htpasswd": true, "netrc": true,
	"authorized_keys": true, "known_hosts": true,
}

// Config/data files whose name hints at secrets (secrets.yaml, service-account.json).
var dataExts = map[string]bool{
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true, ".conf": true,
	".cfg": true, ".xml": true, ".properties": true, ".txt": true, ".plist": true,
}

var secretWords = []string{"secret", "credential", "password", "passwd", "token", "service-account", "serviceaccount", "private"}

var envName = regexp.MustCompile(`\.env($|\.)`) // prod.env, app.env.local

var secretDirs = map[string]bool{"secrets": true, "credentials": true, "certs": true, "keys": true}

func isSecretDir(name string) bool { return secretDirs[strings.ToLower(name)] }

func isSecretFile(name string) bool {
	lower := strings.ToLower(name)
	ext := extOf(lower)
	switch {
	case secretExts[ext], secretNames[lower], envName.MatchString(lower),
		strings.Contains(lower, ".tfstate"), strings.Contains(lower, ".tfvars"): // incl. x.tfvars.json
		return true
	case strings.HasPrefix(lower, "id_rsa"), strings.HasPrefix(lower, "id_dsa"),
		strings.HasPrefix(lower, "id_ecdsa"), strings.HasPrefix(lower, "id_ed25519"):
		return true
	}
	if dataExts[ext] {
		for _, w := range secretWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	return false
}

// Safety net for secrets hardcoded in otherwise normal source files. A file that
// matches any of these is skipped entirely. False positives just mean a file
// doesn't get shown, which is the right trade-off on a stream.
var secretContent = regexp.MustCompile(strings.Join([]string{
	`-----BEGIN [A-Z0-9 ]*PRIVATE KEY`,
	`\b(AKIA|ASIA)[0-9A-Z]{16}\b`,             // AWS access key
	`\bgh[pousr]_[A-Za-z0-9]{36,}`,            // GitHub token
	`\bgithub_pat_[A-Za-z0-9_]{20,}`,          // GitHub fine-grained token
	`\bglpat-[0-9A-Za-z_-]{20,}`,              // GitLab token
	`\bxox[abeprs]-[A-Za-z0-9-]{10,}`,         // Slack token
	`\bxapp-[0-9]-[A-Za-z0-9-]{10,}`,          // Slack app token
	`discord(app)?\.com/api/webhooks/`,        // Discord webhook
	`\bSG\.[\w-]{16,}\.[\w-]{16,}`,            // SendGrid key
	`\bhf_[A-Za-z0-9]{30,}`,                   // Hugging Face token
	`\bpypi-AgE[\w-]{20,}`,                    // PyPI token
	`\bwhsec_[0-9A-Za-z]{16,}`,                // Stripe webhook secret
	`\bAccountKey=[A-Za-z0-9+/=]{40,}`,        // Azure storage connection string
	`\beyJ[\w-]{10,}\.eyJ[\w-]{10,}\.[\w-]+`,  // JWT
	`hooks\.slack\.com/services/`,             // Slack webhook
	`\bsk-(ant-|proj-)?[A-Za-z0-9_-]{20,}`,    // OpenAI / Anthropic style keys
	`\bAIza[0-9A-Za-z_-]{35}`,                 // Google API key
	`\b[sr]k_live_[0-9A-Za-z]{16,}`,           // Stripe live key
	`\bnpm_[A-Za-z0-9]{36}`,                   // npm token
	`"type":\s*"service_account"`,             // GCP service account JSON
	`://[^/\s:@"']+:[^/\s:@"']{6,}@[^/\s"']+`, // creds in a connection URL
	`(?i)(password|passwd|secret|api_?key|access_?token|auth_?token|client_?secret|private_?key)["']?\s*[:=]\s*["'][^"'\s]{12,}["']`,
}, "|"))

func hasSecret(src string) bool { return secretContent.MatchString(src) }
