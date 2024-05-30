package main

import (
	"context"
	"log"
	"net"
	"os/exec"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/connection"
	"google.golang.org/grpc"
)

type server struct {
	pb.ConnectionServiceServer
}

func (s *server) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	// Implement connection logic here
	return &pb.ConnectResponse{Success: true, Message: "Connected"}, nil
}

func (s *server) ExecCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandResponse, error) {
	cmd := exec.Command("sh", "-c", req.Command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &pb.CommandResponse{ExitCode: 1, Stdout: "", Stderr: string(output)}, nil
	}
	return &pb.CommandResponse{ExitCode: 0, Stdout: string(output), Stderr: ""}, nil
}

func (s *server) PutFile(ctx context.Context, req *pb.FileTransferRequest) (*pb.TransferResponse, error) {
	// Implement file transfer logic here
	return &pb.TransferResponse{Success: true, Message: "File transferred"}, nil
}

func (s *server) FetchFile(ctx context.Context, req *pb.FileTransferRequest) (*pb.TransferResponse, error) {
	// Implement file fetching logic here
	return &pb.TransferResponse{Success: true, Message: "File fetched"}, nil
}

func (s *server) Close(ctx context.Context, req *pb.CloseRequest) (*pb.CloseResponse, error) {
	// Implement connection close logic here
	return &pb.CloseResponse{Success: true, Message: "Connection closed"}, nil
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterConnectionServiceServer(s, &server{})

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
