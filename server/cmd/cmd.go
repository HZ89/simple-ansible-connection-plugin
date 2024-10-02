package cmd

import (
	goflag "flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/authenicate"
	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/implement"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/version/verflag"

	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

// Config holds the server configuration
type Config struct {
	WhiteList             []string
	Address               string
	AuthenticatorFilePath string
}

// Execute initializes and starts the gRPC server
func Execute() {

	var cfg Config

	// Initialize flags
	klog.InitFlags(goflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	verflag.AddFlags(pflag.CommandLine)
	pflag.StringSliceVarP(&cfg.WhiteList, "whiteList", "w", []string{}, "Whitelist IPs to allow connection")
	pflag.StringVarP(&cfg.Address, "listen", "l", ":50051", "Address to listen on")
	pflag.StringVarP(&cfg.AuthenticatorFilePath, "authfile", "a", "", "SSH authenticator file path")
	pflag.Parse()

	defer klog.Flush()

	// Listen on the specified address
	lis, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		klog.Fatalf("Failed to listen on %s: %v", cfg.Address, err)
	}
	klog.Infof("Server is listening on %s", cfg.Address)

	// Convert whitelist slice to map for efficient lookup
	whiteMap := make(map[string]bool)
	for _, ip := range cfg.WhiteList {
		whiteMap[ip] = true
	}

	// Initialize SSH Authenticator
	sshAuthenticator, err := authenicate.NewSSHAuthenticator(cfg.AuthenticatorFilePath)
	if err != nil {
		klog.Fatalf("Failed to initialize SSH authenticator: %v", err)
	}
	defer sshAuthenticator.Close()

	// Create server instance
	serverInstance := implement.NewServer(whiteMap, sshAuthenticator)

	// Set up gRPC server options with both unary and streaming interceptors
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(serverInstance.AuthenticateUnary),
		grpc.StreamInterceptor(serverInstance.AuthenticateStream),
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterConnectionServiceServer(grpcServer, serverInstance)

	// Handle graceful shutdown
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			klog.Fatalf("Failed to serve gRPC server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	klog.Info("Shutting down the server gracefully...")
	grpcServer.GracefulStop()
	fmt.Println("Server stopped.")
}
