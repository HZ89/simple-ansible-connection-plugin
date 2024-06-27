import os
import pwd
import sys
import paramiko
import paramiko.message
import base64
# Add the directory of the current file to sys.path
sys.path.append(os.path.dirname(__file__))

import grpc
from grpc import UnaryUnaryClientInterceptor
from ansible.plugins.connection import ConnectionBase, ensure_connect
from ansible.errors import AnsibleConnectionFailure
import connect_pb2
import connect_pb2_grpc
from ansible.utils.display import Display
display = Display()

class AuthInterceptor(UnaryUnaryClientInterceptor):
    def __init__(self, user, password, private_key_path) -> None:
        super().__init__()
        self.user = user
        self.password = password
        self.private_key_path = private_key_path

    def load_private_key(self):
        key_loaders = [
            paramiko.RSAKey,
            paramiko.DSSKey,
            paramiko.ECDSAKey,
            paramiko.Ed25519Key
        ]
        for key_loader in key_loaders:
            try:
                return key_loader(filename=self.private_key_path)
            except paramiko.ssh_exception.PasswordRequiredException:
                raise AnsibleConnectionFailure(f"Key requires a passphrase: {self.private_key_path}")
            except paramiko.ssh_exception.SSHException:
                continue
        raise AnsibleConnectionFailure(f"Failed to load private key: {self.private_key_path}")

    def intercept_unary_unary(self, continuation, client_call_details, request):
        if client_call_details.metadata is None:
            metadata = []
        else:
            metadata = list(client_call_details.metadata)
        metadata.append(('user', self.user))
        if self.password:
            metadata.append(('password', self.password))
        if self.private_key_path:
            try:
                key = self.load_private_key()
            except Exception as e:
                raise AnsibleConnectionFailure(f"Failed to load SSH key: {str(e)}")
            pub_key_algorithm = key.get_name()
            pub_key_fingerprint = key.fingerprint
            metadata.append(('pub-key-algorithm', pub_key_algorithm))
            metadata.append(('pub-key-fingerprint', pub_key_fingerprint))
            try:
                data = self.user.encode('utf-8')
                signature = key.sign_ssh_data(data=data)
                signed_data = base64.b64encode(signature.asbytes()).decode('utf-8')
                metadata.append(('signed-data', signed_data))
            except Exception as e:
                raise AnsibleConnectionFailure(f"Failed to prepare signed ssh data: {str(e)}")
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
        self.user = self._play_context.remote_user or "root"
        self.password = self._play_context.password
        self.private_key_path = self._play_context.private_key_file
        self._connected = False

        if not self.host or not self.port:
            raise AnsibleConnectionFailure("gRPC host and port must be specified")
        display.vvv("grpc_plugin inited")

    def get_ssh_keys(self):
        try:
            user_info = pwd.getpwnam(self.user)
            ssh_dir = os.path.join(user_info.pw_dir, '.ssh')
        except KeyError:
            raise AnsibleConnectionFailure(f"User {self.user} does not exist")

        key_files = []
        if os.path.exists(ssh_dir) and os.path.isdir(ssh_dir):
            for file_name in os.listdir(ssh_dir):
                file_path = os.path.join(ssh_dir, file_name)
                if file_name.startswith('id_') and not file_name.endswith('.pub') and os.path.isfile(file_path):
                    display.vvv(f"add key {file_path} to keys")
                    key_files.append(file_path)
        else:
            raise AnsibleConnectionFailure(f"SSH directory {ssh_dir} does not exist or is not a directory")

        return key_files

    def _connect(self):
        ''' Establish the connection to the remote host '''
        if self._connected:
            display.vvv(f"gRPC connection to host {self.host} already exists")
            return
        display.vvv(f"gRPC trying to connect to host {self.host}:{self.port}")

        key_paths = []
        if self.private_key_path:
            key_paths.append(self.private_key_path)
        else:
            key_paths.extend(self.get_ssh_keys())

        successful_key_path = None
        for key_path in key_paths:
            try:
                channel = grpc.insecure_channel(f'{self.host}:{self.port}')
                auth_interceptor = AuthInterceptor(self.user, self.password, key_path)
                self._channel = grpc.intercept_channel(channel, auth_interceptor)
                self.stub = connect_pb2_grpc.ConnectionServiceStub(self._channel)
                request = connect_pb2.ConnectRequest()
                response = self.stub.Connect(request)
                if response.success:
                    successful_key_path = key_path
                    break
                display.vvv(f"Key {key_path} failed: {response.message}")
            except grpc.RpcError as rpc_error:
                display.vvv(f"got a grpc error: {str(rpc_error)}, key: {key_path}")
                if rpc_error.code() != grpc.StatusCode.PERMISSION_DENIED:
                    raise AnsibleConnectionFailure(f"connect to grpc server failed: {str(rpc_error)}, key: {key_path}")
            except Exception as e:
                display.vvv(f"Failed to use key {key_path}: {str(e)}")
                raise AnsibleConnectionFailure(f"connect to grpc server failed: {str(e)}sc")

        if not successful_key_path:
            raise AnsibleConnectionFailure("No valid SSH key found")

        self.private_key_path = successful_key_path
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