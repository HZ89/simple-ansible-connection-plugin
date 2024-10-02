package implement

import (
	"context"
	"os"
	"path/filepath"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// PutFile method implementation
func (s *Server) PutFile(ctx context.Context, req *pb.PutFileRequest) (*pb.PutFileResponse, error) {
	klog.V(5).InfoS("PutFile request", "remote_path", req.RemotePath)
	klog.V(9).InfoS("File data", "data_length", len(req.FileData))

	auth, err := GetAuthInfoFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	filePath, err := utils.ExpandHomeDirectory(auth.User, req.RemotePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to expend home directory: %v", err)
	}

	klog.V(5).InfoS("Expanded file path", "file_path", filePath)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return &pb.PutFileResponse{Message: err.Error(), Success: false}, nil
	}

	// Write the file with appropriate permissions
	if err := os.WriteFile(filePath, req.FileData, 0644); err != nil {
		return &pb.PutFileResponse{Message: err.Error(), Success: false}, nil
	}

	return &pb.PutFileResponse{Success: true, Message: "File transferred"}, nil
}
