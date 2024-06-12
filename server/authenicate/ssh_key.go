package authenicate

import (
	"fmt"
	"os"
	"os/user"
	"path"

	"golang.org/x/crypto/ssh"
)

type SSHAuthInfo struct {
	SingedData  []byte
	Fingerprint []byte
	Algorithm   string
	Username    string
}

func SSHAuthenticate(i *SSHAuthInfo) (bool, error) {
	publicKey, err := loadPublicKey(i.Username, string(i.Fingerprint))
	if err != nil {
		return false, err
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(i.SingedData, &sig); err != nil {
		return false, err
	}
	if err := publicKey.Verify([]byte(i.Username), &sig); err != nil {
		return false, err
	}
	return true, nil
}

func loadPublicKey(username, finger string) (ssh.PublicKey, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	authorizedKeys, err := os.ReadFile(path.Join(u.HomeDir, ".ssh/authorized_keys"))
	if err != nil {
		return nil, err
	}
	rest := authorizedKeys
	var publicKey ssh.PublicKey
	for len(rest) > 0 {
		publicKey, _, _, rest, err = ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, err
		}
		if ssh.FingerprintSHA256(publicKey) == finger || ssh.FingerprintLegacyMD5(publicKey) == finger {
			return publicKey, nil
		}
	}
	return nil, fmt.Errorf("no public key found")
}
