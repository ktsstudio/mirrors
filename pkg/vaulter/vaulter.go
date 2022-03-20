package vaulter

import (
	"fmt"
	vault "github.com/hashicorp/vault/api"
)

type Vaulter struct {
	client  *vault.Client
	logical *vault.Logical
	auth    *vault.Auth
}

func New(addr string) (*Vaulter, error) {
	config := vault.DefaultConfig()
	config.Address = addr
	client, err := vault.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &Vaulter{
		client:  client,
		logical: client.Logical(),
		auth:    client.Auth(),
	}, nil
}

func (v *Vaulter) Token() string {
	return v.client.Token()
}

func (v *Vaulter) SetToken(token string) {
	v.client.SetToken(token)
}

func (v *Vaulter) LoginAppRole(appRolePath, roleID, secretID string) error {
	appRole := map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	}
	resp, err := v.logical.Write(fmt.Sprintf("auth/%s/login", appRolePath), appRole)
	if err != nil {
		return err
	}
	v.SetToken(resp.Auth.ClientToken)
	return nil
}

func (v *Vaulter) RetrieveData(path string) (map[string]interface{}, error) {
	secret, err := v.logical.Read(path)
	if err != nil {
		return nil, err
	}

	if secret == nil {
		return nil, nil
	}

	if secret.Data == nil || secret.Data["data"] == nil {
		return nil, nil
	}

	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("data type assertion failed: %T %#v", secret.Data["data"], secret.Data["data"])
	}

	return data, nil
}

func (v *Vaulter) RetrieveStringKey(path, key string) (string, error) {
	data, err := v.RetrieveData(path)
	if err != nil {
		return "", err
	}

	value, ok := data[key].(string)
	if !ok {
		return "", fmt.Errorf("value type assertion failed: %T %#v", data[key], data[key])
	}

	return value, nil
}

func (v *Vaulter) WriteData(path string, data map[string]interface{}) error {
	_, err := v.logical.Write(path, data)
	if err != nil {
		return err
	}
	return nil
}
