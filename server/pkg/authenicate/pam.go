package authenicate

import (
	"errors"

	"github.com/msteinert/pam/v2"
	"k8s.io/klog/v2"
)

func PamAuthenticate(user, password string) (bool, error) {
	tx, err := pam.StartFunc("login", user, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.ErrorMsg:
			klog.ErrorS(errors.New(msg), "get a error msg from pam")
			return "", errors.New(msg)
		default:
			return password, nil
		}
	})
	if err != nil {
		klog.V(3).ErrorS(err, "pam exec failed", "user", user)
		return false, err
	}

	if err := tx.Authenticate(0); err != nil {
		klog.V(3).ErrorS(err, "pam auth failed", "user", user)
		return false, nil
	}

	return true, nil
}
