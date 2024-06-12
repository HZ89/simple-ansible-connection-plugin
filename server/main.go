package main

import (
	"context"
	"encoding/base64"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"

	"github.com/HZ89/simple-ansible-connection-plugin/server/authenicate"
	pb "github.com/HZ89/simple-ansible-connection-plugin/server/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

type server struct {
	pb.ConnectionServiceServer
	whiteList map[string]bool
}

func (s *server) Authenticate(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "metadata is nil")
	}
	username, ok := md["user"]
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if len(username) != 1 {
		return nil, status.Errorf(codes.Unauthenticated, "only allowed to authenticate one user")
	}
	password, passwordAuth := md["password"]
	signed, sok := md["signed-data"]
	finger, fok := md["pub-key-fingerprint"]
	algorithm, aok := md["pub-key-algorithm"]
	sshKeyAuth := sok && fok && aok
	if sshKeyAuth && passwordAuth {
		passwordAuth = false
	}

	// confirm user exists
	if _, err := user.Lookup(username[0]); err != nil {
		klog.V(3).ErrorS(err, "user lookup failed", "user", username[0])
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
	var err error
	switch {
	case passwordAuth:
		if pass, err = authenicate.PamAuthenticate(username[0], password[0]); err == nil {
			klog.V(3).InfoS("client try to authentic", "user", username, "clientIP", p.Addr.String(), "passed", pass)
		}

	case sshKeyAuth:
		signedData, err := base64.StdEncoding.DecodeString(signed[0])
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to decode ssh signature data: %v", err)
		}

		pass, err = authenicate.SSHAuthenticate(&authenicate.SSHAuthInfo{
			SingedData:  signedData,
			Fingerprint: []byte(finger[0]),
			Algorithm:   algorithm[0],
			Username:    username[0],
		})

	default:
		if s.whiteList[p.Addr.String()] {
			klog.V(3).InfoS("client already whitelisted", "user", username, "clientIP", p.Addr.String())
			pass = true
		}
	}
	if !pass || err != nil {
		klog.V(3).ErrorS(err, "failed to authenticate", "user", username, "clientIP", p.Addr.String())
		return nil, status.Errorf(codes.PermissionDenied, "authentication failure")
	}
	return handler(ctx, req)
}

func (s *server) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return &pb.ConnectResponse{Success: true, Message: "Connected"}, nil
}

func (s *server) ExecCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandResponse, error) {
	klog.V(5).InfoS("Executing command", "req", klog.Format(req))
	cmd := exec.Command("sh", "-c", req.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &pb.CommandResponse{
			ExitCode: int32(cmd.ProcessState.ExitCode()),
			Stdout:   string(output),
			Stderr:   err.Error(),
		}, nil
	}

	return &pb.CommandResponse{
		ExitCode: int32(cmd.ProcessState.ExitCode()),
		Stdout:   string(output),
		Stderr:   "",
	}, nil
}

func (s *server) PutFile(ctx context.Context, req *pb.PutFileRequest) (*pb.PutFileResponse, error) {
	// Implement file transfer logic here
	klog.V(5).InfoS("Putting file request", "path", req.RemotePath)
	klog.V(9).InfoS("file content", "file_data", string(req.FileData))
	if err := os.WriteFile(req.RemotePath, req.FileData, 777); err != nil {
		return &pb.PutFileResponse{Message: err.Error(), Success: false}, nil
	}
	return &pb.PutFileResponse{Success: true, Message: "File transferred"}, nil
}

func (s *server) FetchFile(ctx context.Context, req *pb.FetchFileRequest) (*pb.FetchFileResponse, error) {
	// Implement file fetching logic here
	klog.V(5).InfoS("Fetching file request", "req", "path", req.RemotePath)
	data, err := os.ReadFile(req.RemotePath)
	if err != nil {
		return &pb.FetchFileResponse{Message: err.Error(), Success: false}, nil
	}
	return &pb.FetchFileResponse{Success: true, Message: "File fetched", FileData: data}, nil
}

func (s *server) Close(ctx context.Context, req *pb.CloseRequest) (*pb.CloseResponse, error) {
	// Implement connection close logic here
	klog.V(5).InfoS("Closing request", "req", klog.Format(req))
	return &pb.CloseResponse{Success: true, Message: "Connection closed"}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}
	server := &server{}
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(server.Authenticate)}

	s := grpc.NewServer(opts...)
	pb.RegisterConnectionServiceServer(s, server)

	klog.InfoS("server listening", "addr", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
