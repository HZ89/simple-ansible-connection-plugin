package implement

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	pb "github.com/HZ89/simple-ansible-connection-plugin/server/pkg/connection"
	"github.com/HZ89/simple-ansible-connection-plugin/server/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"mvdan.cc/sh/v3/shell"
)

func (s *Server) ExecCommand(ctx context.Context, req *pb.CommandRequest) (*pb.CommandResponse, error) {
	klog.V(5).InfoS("Executing command", "req", klog.Format(req))
	auth, err := GetAuthInfoFromContext(ctx)
	if err != nil {
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
	env, err := utils.GetUserEnvFunc(u)
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
