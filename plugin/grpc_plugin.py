import grpc
from ansible.plugins.connection import ConnectionBase
from ansible.errors import AnsibleConnectionFailure
import connection_pb2
import connection_pb2_grpc

class Connection(ConnectionBase):
    ''' gRPC-based connection plugin '''

    transport = 'my_grpc_connection'
    has_pipelining = False
    can_use_pipelining = False

    def __init__(self, *args, **kwargs):
        super(Connection, self).__init__(*args, **kwargs)
        self.host = self._play_context.remote_addr
        self.port = self._play_context.port or 22
        self.user = self._play_context.remote_user
        self.password = self._play_context.password

        self.channel = grpc.insecure_channel('localhost:50051')
        self.stub = connection_pb2_grpc.ConnectionServiceStub(self.channel)

    def connect(self):
        ''' Connect to the remote host '''
        super(Connection, self).connect()
        request = connection_pb2.ConnectRequest(host=self.host, user=self.user, password=self.password, port=self.port)
        response = self.stub.Connect(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)
        self._connected = True

    def exec_command(self, cmd, in_data=None, sudoable=True):
        ''' Run a command on the remote host '''
        if not self._connected:
            raise AnsibleConnectionFailure("Not connected")
        request = connection_pb2.CommandRequest(command=cmd)
        response = self.stub.ExecCommand(request)
        return response.exit_code, response.stdout, response.stderr

    def put_file(self, in_path, out_path):
        ''' Transfer a file from local to remote '''
        request = connection_pb2.FileTransferRequest(local_path=in_path, remote_path=out_path)
        response = self.stub.PutFile(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)

    def fetch_file(self, in_path, out_path):
        ''' Transfer a file from remote to local '''
        request = connection_pb2.FileTransferRequest(local_path=out_path, remote_path=in_path)
        response = self.stub.FetchFile(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)

    def close(self):
        ''' Terminate the connection '''
        request = connection_pb2.CloseRequest()
        response = self.stub.Close(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)