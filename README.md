
# Simple Ansible Connection Plugin

A simple Ansible connection plugin that uses gRPC. This project includes both client and server implementations.

## Features

- gRPC-based connection plugin for Ansible
- SSH key-based authentication
- Dynamic SSH key reloading
- Support for user-specific environment variables
- Systemd service configuration for gRPC server

## Installation

1. Clone the repository:
    ```bash
    git clone https://github.com/HZ89/simple-ansible-connection-plugin.git
    cd simple-ansible-connection-plugin
    ```

2. Build the project:
    ```bash
    make
    ```

3. Ensure the required Python packages are installed:
    ```bash
    pip install paramiko grpcio
    ```

## Configuration

### Dockerfile

- The Dockerfile uses the `golang:1.22-buster` base image. If this image is not accessible, replace it with any suitable
  Golang image from Docker Hub.

### gRPC Plugin Enhancements

- The plugin now includes improved SSH key handling and error management.
- The `grpc_plugin.py` script fetches SSH keys and handles multiple keys dynamically.
- The `SSHAuthenticator` class in `ssh_key.go` uses fsnotify to monitor changes in the authorized keys file and reload
  keys automatically.
- The main server code has been updated to parse structured metadata for authentication and support user-specific
  environment variables during command execution.
- Home directory tilde expansion is implemented for file paths in `PutFile` and `FetchFile` methods.

### Systemd Service

- A systemd service file `ansible-grpc-connection-server.service` is added to manage the gRPC server as a systemd
  service.

## Usage

1. Start the gRPC server:
    ```bash
    ./target/ansible-grpc-connection-server --v 3 -l ":60051"
    ```

2. Configure the client to connect to the gRPC server by setting the appropriate connection parameters in your Ansible
   playbook.

## Systemd Service Setup

1. Copy the systemd service file to `/etc/systemd/system/`:
    ```bash
    cp utils/ansible-grpc-connection-server.service /etc/systemd/system/
    ```

2. Reload systemd manager configuration:
    ```bash
    systemctl daemon-reload
    ```

3. Enable and start the service:
    ```bash
    systemctl enable ansible-grpc-connection-server
    systemctl start ansible-grpc-connection-server
    ```

## Contributing

Contributions are welcome! Please fork the repository and submit a pull request for any improvements or bug fixes.

## License

This project is licensed under the MIT License.
