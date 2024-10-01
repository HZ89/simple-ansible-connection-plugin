package implement

import (
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/authenicate"
	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
)

// Server struct implementing pb.ConnectionServiceServer
type Server struct {
	pb.UnimplementedConnectionServiceServer
	SSHAuthenticator *authenicate.SSHAuthenticator
	WhiteList        map[string]bool
}

// NewServer creates a new Server instance
func NewServer(whiteList map[string]bool, sshAuthenticator *authenicate.SSHAuthenticator) *Server {
	return &Server{
		SSHAuthenticator: sshAuthenticator,
		WhiteList:        whiteList,
	}
}
