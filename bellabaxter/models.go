package bellabaxter

// AllEnvironmentSecretsResponse is returned by GetAllSecrets.
type AllEnvironmentSecretsResponse struct {
	EnvironmentSlug string            `json:"environmentSlug"`
	EnvironmentName string            `json:"environmentName"`
	// Secrets contains all secrets for the environment aggregated from all providers.
	// Served from Baxter's Redis cache — does NOT hit AWS/Azure/GCP per call.
	Secrets         map[string]string `json:"secrets"`
	// Version is a monotonically increasing counter (unix seconds of last mutation).
	Version         int64             `json:"version"`
	// LastModified is an ISO-8601 timestamp of the last mutation.
	LastModified    string            `json:"lastModified"`
}

// EnvironmentSecretsVersionResponse is returned by GetSecretsVersion.
type EnvironmentSecretsVersionResponse struct {
	EnvironmentSlug string `json:"environmentSlug"`
	Version         int64  `json:"version"`
	LastModified    string `json:"lastModified"`
}

// KeyContextResponse is returned by GetKeyContext (GET /api/v1/keys/me).
// It describes which project and environment the API key is scoped to.
type KeyContextResponse struct {
	KeyId           string `json:"keyId"`
	Role            string `json:"role"`
	ProjectId       string `json:"projectId"`
	ProjectSlug     string `json:"projectSlug"`
	ProjectName     string `json:"projectName"`
	EnvironmentId   string `json:"environmentId"`
	EnvironmentSlug string `json:"environmentSlug"`
	EnvironmentName string `json:"environmentName"`
}

// ── SSH models ─────────────────────────────────────────────────────────────────

// SshCaPublicKeyResponse is returned by GetSshCaPublicKey.
type SshCaPublicKeyResponse struct {
	CaPublicKey    string `json:"caPublicKey"`
	Instructions   string `json:"instructions"`
	TerraformSnippet string `json:"terraformSnippet"`
	AnsibleSnippet   string `json:"ansibleSnippet"`
}

// SshRoleResponse describes a single SSH role.
type SshRoleResponse struct {
	Name         string `json:"name"`
	AllowedUsers string `json:"allowedUsers"`
	DefaultTtl   string `json:"defaultTtl"`
	MaxTtl       string `json:"maxTtl"`
}

// SshRolesResponse is returned by ListSshRoles.
type SshRolesResponse struct {
	Roles []SshRoleResponse `json:"roles"`
}

// SshCreateRoleRequest is the body for CreateSshRole.
type SshCreateRoleRequest struct {
	Name         string `json:"name"`
	AllowedUsers string `json:"allowedUsers"`
	DefaultTtl   string `json:"defaultTtl,omitempty"`
	MaxTtl       string `json:"maxTtl,omitempty"`
}

// SshSignRequest is the body for SignSshKey.
type SshSignRequest struct {
	PublicKey       string  `json:"publicKey"`
	RoleName        string  `json:"roleName"`
	Ttl             *string `json:"ttl,omitempty"`
	ValidPrincipals *string `json:"validPrincipals,omitempty"`
}

// SshSignedCertResponse is returned by SignSshKey.
type SshSignedCertResponse struct {
	Success      bool   `json:"success"`
	SignedKey    string `json:"signedKey"`
	SerialNumber string `json:"serialNumber"`
	Instructions string `json:"instructions"`
}

// ── PKI models ─────────────────────────────────────────────────────────────────

// PkiCaResponse is returned by GetPkiCa.
type PkiCaResponse struct {
	Certificate      string  `json:"certificate"`
	CaChain          string  `json:"caChain"`
	Instructions     string  `json:"instructions"`
	AcmeDirectoryUrl *string `json:"acmeDirectoryUrl,omitempty"`
}

// PkiRoleResponse describes a single PKI role.
type PkiRoleResponse struct {
	Name            string `json:"name"`
	AllowedDomains  string `json:"allowedDomains"`
	AllowSubdomains bool   `json:"allowSubdomains"`
	AllowLocalhost  bool   `json:"allowLocalhost"`
	AllowAnyName    bool   `json:"allowAnyName"`
	DefaultTtl      string `json:"defaultTtl"`
	MaxTtl          string `json:"maxTtl"`
	KeyType         string `json:"keyType"`
}

// PkiRolesResponse is returned by ListPkiRoles.
type PkiRolesResponse struct {
	Roles []PkiRoleResponse `json:"roles"`
}

// PkiCreateRoleRequest is the body for CreatePkiRole.
type PkiCreateRoleRequest struct {
	Name            string `json:"name"`
	AllowedDomains  string `json:"allowedDomains"`
	AllowSubdomains bool   `json:"allowSubdomains"`
	AllowLocalhost  bool   `json:"allowLocalhost"`
	AllowAnyName    bool   `json:"allowAnyName"`
	DefaultTtl      string `json:"defaultTtl,omitempty"`
	MaxTtl          string `json:"maxTtl,omitempty"`
	KeyType         string `json:"keyType,omitempty"`
	KeyBits         int    `json:"keyBits,omitempty"`
}

// PkiIssueCertificateRequest is the body for IssuePkiCertificate.
type PkiIssueCertificateRequest struct {
	RoleName   string  `json:"roleName"`
	CommonName string  `json:"commonName"`
	AltNames   *string `json:"altNames,omitempty"`
	IpSans     *string `json:"ipSans,omitempty"`
	Ttl        *string `json:"ttl,omitempty"`
}

// PkiIssuedCertificateResponse is returned by IssuePkiCertificate.
type PkiIssuedCertificateResponse struct {
	Success        bool     `json:"success"`
	Certificate    string   `json:"certificate"`
	PrivateKey     string   `json:"privateKey"`
	PrivateKeyType string   `json:"privateKeyType"`
	IssuingCa      string   `json:"issuingCa"`
	CaChain        []string `json:"caChain"`
	SerialNumber   string   `json:"serialNumber"`
	Expiration     int64    `json:"expiration"`
	Instructions   string   `json:"instructions"`
}
