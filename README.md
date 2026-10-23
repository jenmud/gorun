
# gorun

**CAUTION:** This is still under development and does not work yet!!

Gorun is a deployment orchestrator that executes remote server commands through SSH using a simple declarative configuration file.

## What It Does

* Defines deployment tasks with commands that can be linked together
* Automatically orders and runs tasks in the correct sequence
* Manages SSH connections and authentication
* Loads environment variables from config or .env files
* Runs deployments without requiring SSH installed

## Project Structure

The project consists of the following components:

- `main.go` - The main entry point of the application
- `config/` - Configuration handling and parsing
- `deploy/` - Deployment logic and task execution
- `ssh/` - SSH connection management
- `utils/` - Utility functions

## Features

- Declarative configuration using YAML
- Task dependency resolution
- Secure SSH connection handling
- Environment variable management
- Logging and error handling
- Parallel task execution capabilities

## Configuration

Configuration is done via environment variables. See `.env` for supported environment variable options.

### Environment Variables

- `GORUN_SSH_KEY_PATH` - Path to SSH private key (default: ~/.ssh/id_rsa)
- `GORUN_SSH_USER` - SSH username (default: $USER)
- `GORUN_SSH_PORT` - SSH port (default: 22)
- `GORUN_LOG_LEVEL` - Logging level (default: info)

## Installation

```bash
# Clone the repository
https://github.com/your-username/gorun.git

cd gorun

# Build the project
make build
```

## Usage

```bash
# Run the deployment
./gorun
```

## Development

To contribute to this project:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Submit a pull request

## Development Environment Setup

This project includes a example directory with a Docker Compose setup for testing and development. The environment includes two SSH servers that can be used to test gorun's deployment capabilities.

### Prerequisites

- Docker and Docker Compose installed

### Setting up the Development Environment

1. Start the development environment:

   ```bash
   docker compose up -d
   ```

2. Verify the containers are running:

   ```bash
   docker compose ps
   ```

3. The development environment will expose two SSH servers on ports 2221 and 2222 with the following credentials:
   - Username: testuser
   - Password: testpassword
   - SSH Key: ./ssh-keys/id_ecdsa

### Stopping the Development Environment

To stop the development environment:

```bash
# Stop the containers

docker compose down
```

### Running Tests

To run tests against the development environment:

```bash
# Run the tests
make test
```

You can also run specific tests by specifying a package:

```bash
# Run tests in a specific package
make test PKG=./deploy
```

## Running Gorun Against the Development Environment

To test gorun against the development environment, you'll need to configure it with the appropriate SSH settings.

### Example Configuration

An example configuration file is available in `./example/Gorunfile.yaml`:

Then run:

```bash
$ ./gorun $PWD/example/Gorunfile
```

### Environment Variables

Alternatively, you can use environment variables to configure gorun:

```bash
export GORUN_SSH_USER=testuser
export GORUN_SSH_PASSWORD=testpassword
```

### SSH Access to Test Servers

You can also SSH directly into one of the running test servers using the provided SSH keys:

```bash
# SSH into the first test server
ssh -i ./ssh-keys/id_ecdsa testuser@localhost -p 2221

# SSH into the second test server
ssh -i ./ssh-keys/id_ecdsa testuser@localhost -p 2222
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.
