package main

import (
	"context"
	"encoding/base64"
	"errors"
	goflag "flag"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"github.com/HZ89/simple-ansible-connection-plugin/server/authenicate"
	pb "github.com/HZ89/simple-ansible-connection-plugin/server/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/version/verflag"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"mvdan.cc/sh/v3/shell"
)

type server struct {
	pb.ConnectionServiceServer
	sshAuthenticator *authenicate.SSHAuthenticator
	whiteList        map[string]bool
}

type authInfo struct {
	User              string `json:"user"`
	Password          string `json:"password,omitempty"`
	SignedData        string `json:"signed-data,omitempty"`
	PubKeyFingerprint string `json:"pub-key-fingerprint,omitempty"`
	PubKeyAlgorithm   string `json:"pub-key-algorithm,omitempty"`
}

// parseStructFromMetadata parses a structure from gRPC metadata using JSON tags as keys.
func parseStructFromMetadata(ctx context.Context, result interface{}) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return errors.New("missing metadata in context")
	}

	v := reflect.ValueOf(result)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return errors.New("result argument must be a non-nil pointer")
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		structField := t.Field(i)
		jsonTag := structField.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		// Split the JSON tag to handle omitempty
		tagParts := strings.Split(jsonTag, ",")
		key := tagParts[0]
		omitEmpty := false
		if len(tagParts) > 1 {
			for _, part := range tagParts[1:] {
				if part == "omitempty" {
					omitEmpty = true
				}
			}
		}

		values := md.Get(key)
		if len(values) == 0 {
			if omitEmpty {
				continue // Skip setting this field if it's empty and omitempty is specified
			}
			return errors.New("missing key: " + key)
		}

		if !field.CanSet() {
			return errors.New("cannot set field: " + structField.Name)
		}

		field.SetString(values[0])
	}

	return nil
}

func (s *server) Authenticate(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	var auth authInfo
	if err := parseStructFromMetadata(ctx, &auth); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	// confirm user exists
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
	var err error
	switch {
	case auth.Password != "":
		klog.V(3).InfoS("start password authentication", "user", auth.User)
		if pass, err = authenicate.PamAuthenticate(auth.User, auth.Password); err == nil {
			klog.V(3).InfoS("client try to authentic", "user", auth.User, "clientIP", p.Addr.String(), "passed", pass)
		}

	case auth.PubKeyAlgorithm != "" && auth.PubKeyFingerprint != "" && auth.SignedData != "":
		klog.V(3).InfoS("start ssh key authentication", "user", auth.User)
		signedData, err := base64.StdEncoding.DecodeString(auth.SignedData)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to decode ssh signature data: %v", err)
		}

		pass, err = s.sshAuthenticator.Authenticate(&authenicate.SSHAuthInfo{
			SingedData:  signedData,
			Fingerprint: []byte(auth.PubKeyFingerprint),
			Algorithm:   auth.PubKeyAlgorithm,
			Username:    auth.User,
		})

	default:
		klog.V(3).InfoS("fallback to ip white list", "user", auth.User, "clientIP", p.Addr.String())
		if s.whiteList[p.Addr.String()] {
			klog.V(3).InfoS("client already whitelisted", "user", auth.User, "clientIP", p.Addr.String())
			pass = true
		}
	}
	if !pass || err != nil {
		klog.V(3).ErrorS(err, "failed to authenticate", "user", auth.User, "clientIP", p.Addr.String())
		return nil, status.Errorf(codes.PermissionDenied, "authentication failure")
	}
	return handler(ctx, req)
}

func (s *server) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return &pb.ConnectResponse{Success: true, Message: "Connected"}, nil
}

func getUserEnvFunc(u *user.User) (func(string) string, error) {

	return func(s string) string {
		switch s {
		case "HOME":
			return u.HomeDir
		case "USER":
			return u.Username
		case "LOGNAME":
			return u.Username
		default:
			v, _ := os.LookupEnv(s)
			return v
		}
	}, nil
}

func (s *server) ExecCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandResponse, error) {
	klog.V(5).InfoS("Executing command", "req", klog.Format(req))
	var auth authInfo
	if err := parseStructFromMetadata(ctx, &auth); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}
	u, err := user.Lookup(auth.User)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "user lookup failed: %v", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid uid: %v", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid gid: %v", err)
	}
	env, err := getUserEnvFunc(u)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get user environment: %v", err)
	}
	args, err := shell.Fields(req.Command, env)
	if err != nil {
		return &pb.CommandResponse{
			ExitCode: 255,
			Stdout:   "",
			Stderr:   err.Error(),
		}, nil
	}
	if len(args) == 0 {
		return &pb.CommandResponse{
			ExitCode: 255,
			Stdout:   "",
			Stderr:   "command is empty",
		}, nil
	}
	klog.V(5).InfoS("command will be executed", "args", args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "HOME="+u.HomeDir)
	cmd.Env = append(cmd.Env, "USER="+u.Username)
	cmd.Env = append(cmd.Env, "LOGNAME="+u.Username)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "HOME=") && !strings.HasPrefix(v, "USER=") && !strings.HasPrefix(v, "LOGNAME=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	output, err := cmd.CombinedOutput()
	klog.V(5).InfoS("command result", "stdout", string(output), "stderr", string(output), "err", err, "command", args)
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

func expendHomeTilde(ctx context.Context, filepath string) (string, error) {
	var auth authInfo
	if err := parseStructFromMetadata(ctx, &auth); err != nil {
		return "", err
	}
	u, err := user.Lookup(auth.User)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(filepath, "~/") {
		return strings.Replace("~", filepath, u.HomeDir, 1), nil
	}
	return filepath, nil
}

func (s *server) PutFile(ctx context.Context, req *pb.PutFileRequest) (*pb.PutFileResponse, error) {
	// Implement file transfer logic here
	klog.V(5).InfoS("Putting file request", "path", req.RemotePath)
	klog.V(9).InfoS("file content", "file_data", string(req.FileData))
	filePath, err := expendHomeTilde(ctx, req.RemotePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to expend home directory: %v", err)
	}
	klog.V(5).InfoS("file path after home tilde expend", "file_path", filePath)
	if err := os.WriteFile(filePath, req.FileData, 777); err != nil {
		return &pb.PutFileResponse{Message: err.Error(), Success: false}, nil
	}
	return &pb.PutFileResponse{Success: true, Message: "File transferred"}, nil
}

func (s *server) FetchFile(ctx context.Context, req *pb.FetchFileRequest) (*pb.FetchFileResponse, error) {
	// Implement file fetching logic here
	klog.V(5).InfoS("Fetching file request", "req", "path", req.RemotePath)
	filePath, err := expendHomeTilde(ctx, req.RemotePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to expend home directory: %v", err)
	}
	klog.V(5).InfoS("file path after home tilde expend", "file_path", filePath)
	data, err := os.ReadFile(filePath)
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

var whiteList = make([]string, 0)
var address = ":50051"
var authenticatorFilePath string

func init() {
	klog.InitFlags(goflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	verflag.AddFlags(pflag.CommandLine)
	pflag.StringArrayVarP(&whiteList, "whiteList", "w", whiteList, "white list to allow connection")
	pflag.StringVarP(&address, "liston", "l", address, "address to listen on")
	pflag.StringVarP(&authenticatorFilePath, "authfile", "a", authenticatorFilePath, "ssh authenticator file path")
	pflag.Parse()
}

func main() {
	verflag.PrintAndExitIfRequested("ansible-grpc-connection-server")
	lis, err := net.Listen("tcp", address)
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}
	white := make(map[string]bool)
	for _, w := range whiteList {
		white[w] = true
	}
	sshAuthenticator, err := authenicate.NewSSHAuthenticator(authenticatorFilePath)
	if err != nil {
		klog.Fatalf("failed to init ssh authenticator: %v", err)
	}
	defer sshAuthenticator.Close()
	server := &server{whiteList: white, sshAuthenticator: sshAuthenticator}
	opts := []grpc.ServerOption{grpc.UnaryInterceptor(server.Authenticate)}

	s := grpc.NewServer(opts...)
	pb.RegisterConnectionServiceServer(s, server)

	klog.InfoS("server listening", "addr", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
