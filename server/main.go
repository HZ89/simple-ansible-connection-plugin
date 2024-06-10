package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/connection"
	"github.com/msteinert/pam/v2"
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

func (s *server) pamAuthenticate(user, password string) (bool, error) {
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

func (s *server) Authenticate(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "metadata is nil")
	}
	userName, ok := md["user"]
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
	}
	if len(userName) != 1 {
		return nil, status.Errorf(codes.Unauthenticated, "only allowed to authenticate one user")
	}
	// confirm user exists
	if _, err := user.Lookup(userName[0]); err != nil {
		klog.V(3).ErrorS(err, "user lookup failed", "user", userName[0])
		if _, ok := err.(user.UnknownUserError); ok {
			return nil, status.Errorf(codes.Unauthenticated, "user not authenticated")
		}
		return nil, status.Errorf(codes.Internal, "user lookup failed: %v", err)
	}
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "peer info is nil")
	}
	if s.whiteList[p.Addr.String()] {
		klog.V(3).InfoS("client already whitelisted", "user", userName, "clientIP", p.Addr.String())
		return handler(ctx, req)
	}
	password, ok := md["password"]
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "password not exists")
	}
	if len(password) != 1 {
		return nil, status.Errorf(codes.Unauthenticated, "only allowed to authenticate one password")
	}
	if pass, err := s.pamAuthenticate(userName[0], password[0]); err == nil {
		klog.V(3).InfoS("client try to authentic", "user", userName, "clientIP", p.Addr.String(), "passed", pass)
		if pass {
			return handler(ctx, req)
		}
	} else {
		klog.V(3).ErrorS(err, "client try to authentic failed", "user", userName)
	}
	return nil, status.Errorf(codes.PermissionDenied, "authentication failure")
}

func (s *server) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return &pb.ConnectResponse{Success: true, Message: "Connected"}, nil
}

func (s *server) ExecCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandResponse, error) {
	klog.InfoS("Executing command", "req", klog.Format(req))
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

func (s *server) PutFile(ctx context.Context, req *pb.PutFileRequest) (*pb.PutFIleResponse, error) {
	// Implement file transfer logic here
	klog.InfoS("Putting file request", "path", req.RemotePath)
	klog.V(5).InfoS("file content", "file_data", string(req.FileData))
	if err := os.WriteFile(req.RemotePath, req.FileData, 777); err != nil {
		return &pb.PutFIleResponse{Message: err.Error(), Success: false}, nil
	}
	return &pb.PutFIleResponse{Success: true, Message: "File transferred"}, nil
}

func (s *server) FetchFile(ctx context.Context, req *pb.FetchFileRequest) (*pb.FetchFileResponse, error) {
	// Implement file fetching logic here
	klog.InfoS("Fetching file request", "req", "path", req.RemotePath)
	data, err := os.ReadFile(req.RemotePath)
	if err != nil {
		return &pb.FetchFileResponse{Message: err.Error(), Success: false}, nil
	}
	return &pb.FetchFileResponse{Success: true, Message: "File fetched", FileData: data}, nil
}

func (s *server) Close(ctx context.Context, req *pb.CloseRequest) (*pb.CloseResponse, error) {
	// Implement connection close logic here
	klog.InfoS("Closing request", "req", klog.Format(req))
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
