package implement

import (
	"context"
	"os"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// FetchFile method implementation
func (s *Server) FetchFile(ctx context.Context, req *pb.FetchFileRequest) (*pb.FetchFileResponse, error) {
	klog.V(5).InfoS("FetchFile request", "remote_path", req.RemotePath)

	auth, err := GetAuthInfoFromContext(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	filePath, err := utils.ExpandHomeDirectory(auth.User, req.RemotePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to expend home directory: %v", err)
	}

	klog.V(5).InfoS("Expanded file path", "file_path", filePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return &pb.FetchFileResponse{Message: err.Error(), Success: false}, nil
	}

	return &pb.FetchFileResponse{Success: true, Message: "File fetched", FileData: data}, nil
}
