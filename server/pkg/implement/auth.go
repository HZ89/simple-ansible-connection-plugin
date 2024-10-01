package implement

import (
	"context"
	"encoding/base64"
	"errors"
	"os/user"

	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/authenicate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// AuthInfo struct for authentication metadata
type AuthInfo struct {
	User              string `json:"user"`
	Password          string `json:"password,omitempty"`
	SignedData        string `json:"signed-data,omitempty"`
	PubKeyFingerprint string `json:"pub-key-fingerprint,omitempty"`
	PubKeyAlgorithm   string `json:"pub-key-algorithm,omitempty"`
}

// GetAuthInfoFromContext retrieves authInfo from gRPC metadata
func GetAuthInfoFromContext(ctx context.Context) (*AuthInfo, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, errors.New("missing metadata in context")
	}

	userVals := md.Get("user")
	if len(userVals) == 0 {
		return nil, errors.New("missing 'user' in metadata")
	}

	passwordVals := md.Get("password")
	signedDataVals := md.Get("signed-data")
	pubKeyFingerprintVals := md.Get("pub-key-fingerprint")
	pubKeyAlgorithmVals := md.Get("pub-key-algorithm")

	auth := &AuthInfo{
		User: userVals[0],
	}

	if len(passwordVals) > 0 {
		auth.Password = passwordVals[0]
	}
	if len(signedDataVals) > 0 {
		auth.SignedData = signedDataVals[0]
	}
	if len(pubKeyFingerprintVals) > 0 {
		auth.PubKeyFingerprint = pubKeyFingerprintVals[0]
	}
	if len(pubKeyAlgorithmVals) > 0 {
		auth.PubKeyAlgorithm = pubKeyAlgorithmVals[0]
	}

	return auth, nil
}

// AuthenticateUnary is a unary interceptor for authentication
func (s *Server) AuthenticateUnary(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	auth, err := GetAuthInfoFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	// Confirm user exists
	if _, err := user.Lookup(auth.User); err != nil {
		klog.V(3).ErrorS(err, "user lookup failed", "user", auth.User)
		if _, ok := err.(user.UnknownUserError); ok {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		return nil, status.Errorf(codes.Internal, "user lookup failed: %v", err)
	}

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "peer info is nil")
	}

	var pass bool
	var authErr error
	switch {
	case auth.Password != "":
		klog.V(3).InfoS("Starting password authentication", "user", auth.User)
		if pass, authErr = authenicate.PamAuthenticate(auth.User, auth.Password); authErr == nil {
			klog.V(3).InfoS("Authentication attempt", "user", auth.User, "clientIP", p.Addr.String(), "passed", pass)
		}

	case auth.PubKeyAlgorithm != "" && auth.PubKeyFingerprint != "" && auth.SignedData != "":
		klog.V(3).InfoS("Starting SSH key authentication", "user", auth.User)
		signedData, authErr := base64.StdEncoding.DecodeString(auth.SignedData)
		if authErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to decode SSH signature data: %v", authErr)
		}

		pass, authErr = s.SSHAuthenticator.Authenticate(&authenicate.SSHAuthInfo{
			SignedData:  signedData,
			Fingerprint: []byte(auth.PubKeyFingerprint),
			Algorithm:   auth.PubKeyAlgorithm,
			Username:    auth.User,
		})

	default:
		klog.V(3).InfoS("Falling back to IP whitelist", "user", auth.User, "clientIP", p.Addr.String())
		if s.WhiteList[p.Addr.String()] {
			klog.V(3).InfoS("Client is whitelisted", "user", auth.User, "clientIP", p.Addr.String())
			pass = true
		}
	}

	if !pass || authErr != nil {
		klog.V(3).ErrorS(authErr, "Authentication failed", "user", auth.User, "clientIP", p.Addr.String())
		return nil, status.Errorf(codes.PermissionDenied, "authentication failure")
	}

	return handler(ctx, req)
}

// AuthenticateStream is a streaming interceptor for authentication
func (s *Server) AuthenticateStream(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := ss.Context()
	auth, err := GetAuthInfoFromContext(ctx)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	// Confirm user exists
	if _, err := user.Lookup(auth.User); err != nil {
		klog.V(3).ErrorS(err, "user lookup failed", "user", auth.User)
		if _, ok := err.(user.UnknownUserError); ok {
			return status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		return status.Errorf(codes.Internal, "user lookup failed: %v", err)
	}

	p, ok := peer.FromContext(ctx)
	if !ok {
		return status.Errorf(codes.Internal, "peer info is nil")
	}

	var pass bool
	var authErr error
	switch {
	case auth.Password != "":
		klog.V(3).InfoS("Starting password authentication", "user", auth.User)
		if pass, authErr = authenicate.PamAuthenticate(auth.User, auth.Password); authErr == nil {
			klog.V(3).InfoS("Authentication attempt", "user", auth.User, "clientIP", p.Addr.String(), "passed", pass)
		}

	case auth.PubKeyAlgorithm != "" && auth.PubKeyFingerprint != "" && auth.SignedData != "":
		klog.V(3).InfoS("Starting SSH key authentication", "user", auth.User)
		signedData, authErr := base64.StdEncoding.DecodeString(auth.SignedData)
		if authErr != nil {
			return status.Errorf(codes.Internal, "failed to decode SSH signature data: %v", authErr)
		}

		pass, authErr = s.SSHAuthenticator.Authenticate(&authenicate.SSHAuthInfo{
			SignedData:  signedData,
			Fingerprint: []byte(auth.PubKeyFingerprint),
			Algorithm:   auth.PubKeyAlgorithm,
			Username:    auth.User,
		})

	default:
		klog.V(3).InfoS("Falling back to IP whitelist", "user", auth.User, "clientIP", p.Addr.String())
		if s.WhiteList[p.Addr.String()] {
			klog.V(3).InfoS("Client is whitelisted", "user", auth.User, "clientIP", p.Addr.String())
			pass = true
		}
	}

	if !pass || authErr != nil {
		klog.V(3).ErrorS(authErr, "Authentication failed", "user", auth.User, "clientIP", p.Addr.String())
		return status.Errorf(codes.PermissionDenied, "authentication failure")
	}

	return handler(srv, ss)
}
