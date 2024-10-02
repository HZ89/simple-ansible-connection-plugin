package implement

import (
	"context"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"k8s.io/klog/v2"
)

// Connect method implementation
func (s *Server) Connect(ctx context.Context, req *pb.ConnectRequest) (*pb.ConnectResponse, error) {
	return &pb.ConnectResponse{Success: true, Message: "Connected"}, nil
}

// Close method implementation
func (s *Server) Close(ctx context.Context, req *pb.CloseRequest) (*pb.CloseResponse, error) {
	klog.V(5).InfoS("Close request received", "request", req)
	return &pb.CloseResponse{Success: true, Message: "Connection closed"}, nil
}
