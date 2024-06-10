# Simple Ansible Connection Plugin

This repository provides a gRPC-based Ansible connection plugin that supports SSH key-based challenge-response
authentication. The plugin and server are implemented in Python and Go, respectively.

## Usage

### Building the Server

To build the gRPC server, use the provided Makefile(only support linux):

```sh
make build
```

### Running the Server

You can run the server using Docker:

```sh
docker build -t grpc-server .
docker run -p 50051:50051 grpc-server
```

### Ansible Configuration

Update your Ansible inventory file (`inventory/hosts.ini`) with the target hosts and connection details:

```ini
[my_hosts]
127.0.0.1 ansible_port=50051 ansible_user=my_user ansible_connection=grpc_plugin
```

### Using the Plugin

Execute Ansible commands using the custom gRPC connection plugin:

```sh
ANSIBLE_CONNECTION_PLUGINS=./plugin ansible -i ./inventory/hosts.ini my_hosts -m command -a "echo 'Hello from gRPC connection plugin'" -vvv
```

## Contributing

Contributions are welcome! Please open an issue or submit a pull request for any improvements or bug fixes.

## License

This project is licensed under the MIT License.
