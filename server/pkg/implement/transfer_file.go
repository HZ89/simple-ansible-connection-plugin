// server/pkg/implement/transfer_file.go
package implement

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)

// TransferFile implements the bidirectional streaming RPC for file transfers.
func (s *Server) TransferFile(stream pb.ConnectionService_TransferFileServer) error {
	klog.V(3).InfoS("TransferFile stream initiated")

	// Receive the first message to determine the operation.
	firstMsg, err := stream.Recv()
	if err != nil {
		klog.ErrorS(err, "Failed to receive initial message in TransferFile")
		return status.Errorf(codes.InvalidArgument, "failed to receive initial message: %v", err)
	}

	switch payload := firstMsg.Payload.(type) {
	case *pb.FileTransferMessage_Control:
		switch payload.Control.Operation {
		case pb.ControlMessage_UPLOAD:
			klog.V(4).InfoS("Handling file upload operation", "remote_path", payload.Control.Info.RemotePath)
			return s.handleUpload(stream, payload.Control.Info)
		case pb.ControlMessage_DOWNLOAD:
			klog.V(4).InfoS("Handling file download operation", "remote_path", payload.Control.Info.RemotePath)
			return s.handleDownload(stream, payload.Control.Info)
		default:
			errMsg := fmt.Sprintf("unknown operation: %v", payload.Control.Operation)
			klog.ErrorS(nil, errMsg, "operation", payload.Control.Operation)
			return status.Errorf(codes.InvalidArgument, errMsg)
		}
	default:
		errMsg := "expected ControlMessage as first message"
		klog.ErrorS(nil, errMsg, "received_type", fmt.Sprintf("%T", firstMsg.Payload))
		return status.Errorf(codes.InvalidArgument, errMsg)
	}
}

// handleUpload manages the upload process with detailed logging.
func (s *Server) handleUpload(stream pb.ConnectionService_TransferFileServer, info *pb.FileInfo) error {
	if info == nil {
		errMsg := "missing FileInfo in upload"
		klog.ErrorS(nil, errMsg)
		return status.Errorf(codes.InvalidArgument, errMsg)
	}

	klog.V(3).InfoS("Starting file upload", "local_path", info.LocalPath, "remote_path", info.RemotePath, "file_size", info.FileSize)

	auth, err := GetAuthInfoFromContext(stream.Context())
	if err != nil {
		klog.ErrorS(err, "Failed to retrieve auth info for upload")
		return status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	// Expand tilde in file path
	filePath, err := utils.ExpandHomeDirectory(auth.User, info.RemotePath)
	if err != nil {
		klog.ErrorS(err, "Failed to expand home directory for upload", "remote_path", info.RemotePath)
		return status.Errorf(codes.Internal, "failed to expand home directory: %v", err)
	}

	klog.V(4).InfoS("Expanded file path for upload", "file_path", filePath)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		klog.ErrorS(err, "Failed to create directories for upload", "dir", filepath.Dir(filePath))
		return status.Errorf(codes.Internal, "failed to create directories: %v", err)
	}

	// Create the file with appropriate permissions
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		klog.ErrorS(err, "Failed to create file for upload", "file_path", filePath)
		return status.Errorf(codes.Internal, "failed to create file: %v", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			klog.ErrorS(cerr, "Failed to close file after upload", "file_path", filePath)
		}
	}()

	var receivedBytes int64
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			klog.V(3).InfoS("File upload completed", "file_path", filePath, "bytes_received", receivedBytes)
			break
		}
		if err != nil {
			klog.ErrorS(err, "Failed to receive file chunk during upload", "file_path", filePath, "bytes_received", receivedBytes)
			return status.Errorf(codes.Unknown, "failed to receive file chunk: %v", err)
		}

		dataPayload, ok := msg.Payload.(*pb.FileTransferMessage_Data)
		if !ok {
			errMsg := "expected FileData message during upload"
			klog.ErrorS(nil, errMsg, "received_type", fmt.Sprintf("%T", msg.Payload))
			return status.Errorf(codes.InvalidArgument, errMsg)
		}

		// Write the chunk to the file
		n, err := file.Write(dataPayload.Data.Data)
		if err != nil {
			klog.ErrorS(err, "Failed to write to file during upload", "file_path", filePath, "bytes_received", receivedBytes)
			return status.Errorf(codes.Internal, "failed to write to file: %v", err)
		}
		receivedBytes += int64(n)
		klog.V(5).InfoS("Received and wrote file chunk", "file_path", filePath, "bytes_written", n, "total_received", receivedBytes)
	}

	// Optionally, verify the file size
	if info.FileSize > 0 && receivedBytes != info.FileSize {
		errMsg := fmt.Sprintf("file size mismatch: expected %d bytes, received %d bytes", info.FileSize, receivedBytes)
		klog.ErrorS(nil, errMsg, "file_path", filePath)
		return status.Errorf(codes.DataLoss, errMsg)
	}

	uid, gid, err := utils.GetUserIDs(auth.User)
	if err != nil {
		klog.ErrorS(err, "Failed to lookup user", "user", auth.User)
		return status.Errorf(codes.Internal, "failed to lookup user: %v", err)
	}

	if err := os.Chown(filePath, uid, gid); err != nil {
		klog.ErrorS(err, "Failed to change ownership of file", "file_path", filePath)
		return status.Errorf(codes.Internal, "failed to change ownership of file: %v", err)
	}

	// Send a final ControlMessage as acknowledgment
	ackMsg := &pb.FileTransferMessage{
		Payload: &pb.FileTransferMessage_Control{
			Control: &pb.ControlMessage{
				Operation: pb.ControlMessage_UPLOAD,
				Info: &pb.FileInfo{
					LocalPath:  info.LocalPath,
					RemotePath: info.RemotePath,
					FileSize:   receivedBytes,
				},
			},
		},
	}

	if err := stream.Send(ackMsg); err != nil {
		klog.ErrorS(err, "Failed to send acknowledgment after upload", "file_path", filePath)
		return status.Errorf(codes.Unknown, "failed to send acknowledgment: %v", err)
	}
	klog.V(3).InfoS("Acknowledgment sent after file upload", "file_path", filePath)

	return nil
}

// handleDownload manages the download process with detailed logging.
func (s *Server) handleDownload(stream pb.ConnectionService_TransferFileServer, info *pb.FileInfo) error {
	if info == nil {
		errMsg := "missing FileInfo in download"
		klog.ErrorS(nil, errMsg)
		return status.Errorf(codes.InvalidArgument, errMsg)
	}

	klog.V(3).InfoS("Starting file download", "local_path", info.LocalPath, "remote_path", info.RemotePath)

	auth, err := GetAuthInfoFromContext(stream.Context())
	if err != nil {
		klog.ErrorS(err, "Failed to retrieve auth info for download")
		return status.Errorf(codes.Unauthenticated, "invalid auth info: %v", err)
	}

	// Expand tilde in file path
	filePath, err := utils.ExpandHomeDirectory(auth.User, info.RemotePath)
	if err != nil {
		klog.ErrorS(err, "Failed to expand home directory for download", "remote_path", info.RemotePath)
		return status.Errorf(codes.Internal, "failed to expand home directory: %v", err)
	}

	klog.V(4).InfoS("Expanded file path for download", "file_path", filePath)

	// Open the file for reading
	file, err := os.Open(filePath)
	if err != nil {
		klog.ErrorS(err, "Failed to open file for download", "file_path", filePath)
		return status.Errorf(codes.NotFound, "failed to open file: %v", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			klog.ErrorS(cerr, "Failed to close file after download", "file_path", filePath)
		}
	}()

	// Get file info to determine size
	fileStat, err := file.Stat()
	if err != nil {
		klog.ErrorS(err, "Failed to stat file for download", "file_path", filePath)
		return status.Errorf(codes.Internal, "failed to stat file: %v", err)
	}

	klog.V(4).InfoS("File info retrieved for download", "file_path", filePath, "file_size", fileStat.Size())

	// Send ControlMessage with FileInfo
	controlMsg := &pb.FileTransferMessage{
		Payload: &pb.FileTransferMessage_Control{
			Control: &pb.ControlMessage{
				Operation: pb.ControlMessage_DOWNLOAD,
				Info: &pb.FileInfo{
					LocalPath:  info.LocalPath,
					RemotePath: info.RemotePath,
					FileSize:   fileStat.Size(),
				},
			},
		},
	}

	if err := stream.Send(controlMsg); err != nil {
		klog.ErrorS(err, "Failed to send ControlMessage for download", "file_path", filePath)
		return status.Errorf(codes.Unknown, "failed to send control message: %v", err)
	}
	klog.V(3).InfoS("ControlMessage sent for download", "file_path", filePath)

	// Stream the file in chunks
	buffer := make([]byte, 32*1024) // 32KB chunks
	var sentBytes int64
	for {
		n, err := file.Read(buffer)
		if err == io.EOF {
			klog.V(3).InfoS("File download completed", "file_path", filePath, "bytes_sent", sentBytes)
			break
		}
		if err != nil {
			klog.ErrorS(err, "Failed to read file chunk during download", "file_path", filePath, "bytes_sent", sentBytes)
			return status.Errorf(codes.Internal, "failed to read file: %v", err)
		}

		dataMsg := &pb.FileTransferMessage{
			Payload: &pb.FileTransferMessage_Data{
				Data: &pb.FileData{
					Data: buffer[:n],
				},
			},
		}

		if err := stream.Send(dataMsg); err != nil {
			klog.ErrorS(err, "Failed to send file chunk during download", "file_path", filePath, "bytes_sent", sentBytes)
			return status.Errorf(codes.Unknown, "failed to send file chunk: %v", err)
		}
		sentBytes += int64(n)
		klog.V(5).InfoS("Sent file chunk", "file_path", filePath, "bytes_sent", n, "total_sent", sentBytes)
	}

	return nil
}
