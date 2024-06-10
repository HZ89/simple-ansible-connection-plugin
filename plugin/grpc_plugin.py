import os
import sys

# Add the directory of the current file to sys.path
sys.path.append(os.path.dirname(__file__))

import grpc
from grpc import UnaryUnaryClientInterceptor, ClientCallDetails
from grpc.beta import implementations
from ansible.plugins.connection import ConnectionBase, ensure_connect
from ansible.errors import AnsibleConnectionFailure
import connect_pb2
import connect_pb2_grpc
from ansible.utils.display import Display
display = Display()

class AuthInterceptor(UnaryUnaryClientInterceptor):
    def __init__(self, user, password) -> None:
        super().__init__()
        self.user = user
        self.password = password
    
    def intercept_unary_unary(self, continuation, client_call_details, request):
        if client_call_details.metadata is None:
            metadata = []
        else:
            metadata = list(client_call_details.metadata)
        metadata.append(('user', self.user))
        metadata.append(('password', self.password))
        client_call_details = client_call_details._replace(metadata=metadata)
        return continuation(client_call_details, request)


class Connection(ConnectionBase):
    ''' gRPC-based connection plugin '''

    transport = 'grpc_plugin'
    has_pipelining = False
    can_use_pipelining = False

    def __init__(self, play_context, new_stdin,*args, **kwargs):
        super(Connection, self).__init__(play_context, new_stdin,*args, **kwargs)
        self.host = self._play_context.remote_addr
        self.port = self._play_context.port or 50051
        self.user = self._play_context.remote_user
        self.password = self._play_context.password
        self._connected = False

        if not self.host or not self.port:
            raise AnsibleConnectionFailure("gRPC host and port must be specified")
        display.vvv("grpc_plugin inited")
        self.set_option("persistent_log_messages", "false")

    def _connect(self):
        ''' Establish the connection to the remote host '''
        if self.connected:
            display.vvv("grpc connection to host {} already exist".format(self.host))
            return
        display.vvv("grpc try connect to host {}:{}".format(self.host, self.port))
        channel =  grpc.insecure_channel(f'{self.host}:{self.port}')
        auth_interceptor = AuthInterceptor(self.user, self.password)
        self._channel = grpc.intercept_channel(channel, auth_interceptor)
        self.stub = connect_pb2_grpc.ConnectionServiceStub(self._channel)
        request = connect_pb2.ConnectRequest()
        response = self.stub.Connect(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)
        self._connected = True

    @ensure_connect
    def exec_command(self, cmd, in_data=None, sudoable=True):
        ''' Run a command on the remote host '''
        display.vvv("Exec command {}".format(cmd))
        if not self._connected:
            raise AnsibleConnectionFailure("Not connected")
        request = connect_pb2.CommandRequest(command=cmd)
        response = self.stub.ExecCommand(request)
        return response.exit_code, response.stdout, response.stderr

    @ensure_connect
    def put_file(self, in_path, out_path):
        ''' Transfer a file from local to remote '''
        display.vvv("Putting file from {} to {}".format(in_path, out_path))
        with open(in_path, 'rb') as f:
            file_data = f.read()
        request = connect_pb2.PutFileRequest(remote_path=out_path, file_data=file_data)
        response = self.stub.PutFile(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)
        display.vvv("Successfully put file to {}".format(out_path))

    @ensure_connect
    def fetch_file(self, in_path, out_path):
        ''' Transfer a file from remote to local '''
        display.vvv("Fetching file from {} to {}".format(in_path, out_path))
        request = connect_pb2.FetchFileRequest(remote_path=in_path)
        response = self.stub.FetchFile(request)
        if response.success:
            with open(out_path, 'wb') as f:
                f.write(response.file_data)
                f.flush()
                f.close()
                os.fsync(f.fileno())
            display.vvv("Successfully fetched file to {}".format(out_path))
        else:
            raise AnsibleConnectionFailure(response.message)

    def close(self):
        ''' Terminate the connection '''
        if self._connected:
            self.queue_message("vvv", "closing grpc connection to host")
            self._chnnel.close()
        self._connected = False