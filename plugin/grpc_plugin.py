import os
import sys

# Add the directory of the current file to sys.path
sys.path.append(os.path.dirname(__file__))

import grpc
from grpc.beta import implementations
from ansible.plugins.connection import ConnectionBase, ensure_connect
from ansible.errors import AnsibleConnectionFailure
import connect_pb2
import connect_pb2_grpc
from ansible.utils.display import Display
display = Display()


class Connection(ConnectionBase):
    ''' gRPC-based connection plugin '''

    transport = 'grpc_plugin'
    has_pipelining = False
    can_use_pipelining = False

    def __init__(self, play_context, new_stdin,*args, **kwargs):
        super(Connection, self).__init__(play_context, new_stdin,*args, **kwargs)
        self.host = self._play_context.remote_addr
        self.port = self._play_context.port or 22
        self.user = self._play_context.remote_user
        self.password = self._play_context.password

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
        self._channel =  grpc.insecure_channel(f'{self.host}:{self.port}')
        self.stub = connect_pb2_grpc.ConnectionServiceStub(self._channel)
        request = connect_pb2.ConnectRequest(host=self.host, user=self.user, password=self.password, port=self.port)
        response = self.stub.Connect(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)
        self._connected = True

    @ensure_connect
    def exec_command(self, cmd, in_data=None, sudoable=True):
        ''' Run a command on the remote host '''
        if not self._connected:
            raise AnsibleConnectionFailure("Not connected")
        request = connect_pb2.CommandRequest(command=cmd)
        response = self.stub.ExecCommand(request)
        return response.exit_code, response.stdout, response.stderr

    @ensure_connect
    def put_file(self, in_path, out_path):
        ''' Transfer a file from local to remote '''
        request = connect_pb2.PutFileRequest(local_path=in_path, remote_path=out_path)
        response = self.stub.PutFile(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)

    @ensure_connect
    def fetch_file(self, in_path, out_path):
        ''' Transfer a file from remote to local '''
        request = connect_pb2.FetchFileRequest(local_path=out_path, remote_path=in_path)
        response = self.stub.FetchFile(request)
        if not response.success:
            raise AnsibleConnectionFailure(response.message)

    def close(self):
        ''' Terminate the connection '''
        if self._connected:
            self.queue_message("vvv", "closing grpc connection to host")
            self._chnnel.close()
        self._connected = False