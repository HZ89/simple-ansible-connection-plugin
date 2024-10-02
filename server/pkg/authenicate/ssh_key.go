package authenicate

import (
	"fmt"
	"os"
	"os/user"
	"path"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/crypto/ssh"
	"k8s.io/klog/v2"
)

type SSHAuthenticator struct {
	authorizedKeys     sync.Map // map[string]sync.Map map[string]ssh.PublicKey
	authorizedFilePath string
	watcher            *fsnotify.Watcher
	subWriteEventChans map[string]chan fsnotify.Op
}

func NewSSHAuthenticator(authorizedFilePath string) (*SSHAuthenticator, error) {

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	authenicator := &SSHAuthenticator{
		authorizedKeys:     sync.Map{},
		authorizedFilePath: authorizedFilePath,
		watcher:            w,
		subWriteEventChans: make(map[string]chan fsnotify.Op),
	}
	if authorizedFilePath != "" {
		if err = w.Add(authorizedFilePath); err != nil {
			return nil, fmt.Errorf("error adding authorized keys file %q: %w", authorizedFilePath, err)
		}
		if err = authenicator.loadAuthenticateKeysFromFile(authorizedFilePath); err != nil {
			return nil, fmt.Errorf("error loading authorized keys file %q: %w", authorizedFilePath, err)
		}
	}

	go authenicator.watchFile()
	return &SSHAuthenticator{
		authorizedKeys:     sync.Map{},
		authorizedFilePath: authorizedFilePath,
		watcher:            w,
	}, nil
}

func (s *SSHAuthenticator) watchFile() {
	for {
		select {
		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			switch {
			case event.Op.Has(fsnotify.Write):
				s.writeEventHandler(event)
			case event.Op.Has(fsnotify.Remove):
				s.deleteAuthenticateKeys(event.Name)
			}
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			klog.Errorln(err, "error watching file")
		}
	}
}

func (s *SSHAuthenticator) writeEventHandler(event fsnotify.Event) {
	ch, ok := s.subWriteEventChans[event.Name]
	if !ok {
		ch = make(chan fsnotify.Op)
		s.subWriteEventChans[event.Name] = ch
		go func() {
			filePath := event.Name
			timer := time.NewTimer(time.Second)
			for {
				select {
				case op := <-ch:
					if !timer.Reset(time.Second) {
						klog.V(5).ErrorS(fmt.Errorf("timer expired"), "key", event.Name, "op", op)
					}
				case <-timer.C:
					if err := s.loadAuthenticateKeysFromFile(filePath); err != nil {
						klog.ErrorS(err, "failed to load authenticate keys from file", "file", filePath)
					}
					return
				}
			}
		}()
	}
	ch <- fsnotify.Write
}

func (s *SSHAuthenticator) loadAuthenticateKeysFromFile(f string) error {
	content, err := os.ReadFile(f)
	if err != nil {
		return fmt.Errorf("error reading file %q: %w", f, err)
	}
	rest := content
	var pk ssh.PublicKey
	var pks sync.Map
	for len(rest) > 0 {
		pk, _, _, rest, err = ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return fmt.Errorf("error parsing file %q: %w", f, err)
		}
		pks.Store(ssh.FingerprintSHA256(pk), pk)
	}
	s.authorizedKeys.Store(f, &pks)
	return nil
}

func (s *SSHAuthenticator) deleteAuthenticateKeys(f string) {
	s.authorizedKeys.Delete(f)
}

func (s *SSHAuthenticator) Close() error {
	return s.watcher.Close()
}

func (s *SSHAuthenticator) loadPublicKey(username, finger string) (ssh.PublicKey, error) {
	authenticateFilePath := s.authorizedFilePath
	if authenticateFilePath == "" {
		u, err := user.Lookup(username)
		if err != nil {
			return nil, fmt.Errorf("error looking up username %q: %w", username, err)
		}
		authenticateFilePath = path.Join(u.HomeDir, ".ssh", "authorized_keys")
	}
	value, ok := s.authorizedKeys.Load(authenticateFilePath)
	if !ok {
		if err := s.loadAuthenticateKeysFromFile(authenticateFilePath); err != nil {
			return nil, fmt.Errorf("error loading authorized keys file %q: %w", authenticateFilePath, err)
		}
		if err := s.watcher.Add(authenticateFilePath); err != nil {
			return nil, fmt.Errorf("error adding authorized keys file %q: %w", authenticateFilePath, err)
		}
		value, _ = s.authorizedKeys.Load(authenticateFilePath)
	}
	v, ok := value.(*sync.Map).Load(finger)
	if !ok {
		return nil, fmt.Errorf("failed to load public key from file %q", authenticateFilePath)
	}
	return v.(ssh.PublicKey), nil
}

func (s *SSHAuthenticator) Authenticate(info *SSHAuthInfo) (bool, error) {
	publicKey, err := s.loadPublicKey(info.Username, string(info.Fingerprint))
	if err != nil {
		return false, err
	}
	var sig ssh.Signature
	if err := ssh.Unmarshal(info.SignedData, &sig); err != nil {
		return false, fmt.Errorf("error parsing ssh signature: %w", err)
	}
	if err := publicKey.Verify([]byte(info.Username), &sig); err != nil {
		return false, fmt.Errorf("error verifying ssh signature: %w", err)
	}
	return true, nil
}

type SSHAuthInfo struct {
	SignedData  []byte
	Fingerprint []byte
	Algorithm   string
	Username    string
}
