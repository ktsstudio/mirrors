package backend

type VaultBackend interface {
	Token() string
	SetToken(token string)
	LoginAppRole(appRolePath, roleID, secretID string) error
	RetrieveData(path string) (map[string]interface{}, error)
	WriteData(path string, data map[string]interface{}) error
}
