# Go-GitSync: Decentralized, Collaborative File Synchronization with Git Semantics

[![Go Report Card](https://goreportcard.com/badge/github.com/your-username/gitsync)](https://goreportcard.com/report/github.com/your-username/gitsync) <!-- Replace with your actual Go Report Card link once repo is public -->
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE) <!-- Update License Badge if using a different license -->

## Project Description

Go-GitSync is a novel decentralized file synchronization tool built in Go. It leverages the well-established and robust semantics of Git version control to provide a collaborative and versioned file synchronization solution. Unlike traditional centralized or custom protocol-based decentralized sync tools, Go-GitSync focuses on structured collaboration, version history, and conflict resolution in a manner familiar to Git users.

**Key Features:**

*   **Decentralized Synchronization:** No central server required. Peers directly synchronize with each other.
*   **Git-Based Version Control:** Uses Git's core concepts (commits, branches, merges) for managing changes and history.
*   **Collaborative Workflow:** Designed for teams working together, enabling branching, merging, and conflict resolution like in Git.
*   **Offline-First:** Work offline and synchronize changes when peers are available.
*   **Version History & Audit Trail:** Every change is tracked, providing a complete history and easy rollback.
*   **Branching & Merging:** Supports Git-style branching and merging for parallel work and feature development.
*   **Secure & Private (Optional):** Potential for end-to-end encryption between peers.
*   **Resilient & Fault-Tolerant:** Decentralized nature increases resilience and eliminates single points of failure.
*   **Extensible & Customizable:** Built in Go, allowing for easy extension and integration.

## Getting Started

### Prerequisites

*   **Go:** Go 1.18 or later is required. You can download it from [https://go.dev/dl/](https://go.dev/dl/).
*   **Git:** Git must be installed on your system as Go-GitSync leverages local Git repositories. You can download it from [https://git-scm.com/downloads](https://git-scm.com/downloads).

### Installation

1.  **Clone the repository:**

    ```bash
    git clone git@github.com:jmendick/gitsync.git  # Replace with your repository URL
    cd gitsync
    ```

2.  **Build the `gitsync` command-line tool:**

    ```bash
    go build -o bin/gitsync ./cmd/gitsync/main.go
    ```

    This will create an executable binary named `gitsync` in the `bin` directory. Ensure the `bin` directory is in your system's `$PATH` for easy access.

### Running Go-GitSync

1.  **Initialize a Go-GitSync repository:**

    Navigate to the directory you want to synchronize and run:

    ```bash
    gitsync init <repository-name>
    ```

    This command (not yet implemented - will be added in future development) will initialize a local Git repository and the necessary Go-GitSync configurations within that directory.

2.  **Run the `gitsync` application:**

    From the project root directory or after adding `bin` to your `$PATH`, you can run:

    ```bash
    gitsync
    ```

    By default, Go-GitSync will start listening for peer connections on `:8080` and store repository data in `./gitsync-repos`.

    You can customize the configuration using command-line flags:

    ```bash
    gitsync --listen=":9090" --repo-dir="/path/to/my/repos"
    ```

    See the "Usage" section for more details on available commands and flags (to be developed).

## Usage (Planned)

### Command-Line Interface (CLI):

Go-GitSync will provide a CLI tool (`gitsync`) for managing repositories, peers, and synchronization:

*   `gitsync init <repository-name>`: Initializes a new Go-GitSync repository in the current directory.
*   `gitsync share <repository-name>`: Shares a local repository with other peers.
*   `gitsync sync <repository-name>`: Manually triggers synchronization for a repository.
*   `gitsync peers`: Lists connected and known peers.
*   `gitsync status <repository-name>`: Shows the synchronization status of a repository.
*   `gitsync config`: Displays or modifies Go-GitSync configuration.
*   `gitsync --help`: Displays help information for all commands and flags.

### Configuration:

Go-GitSync can be configured via:

*   Command-line flags
*   YAML configuration file
*   Runtime configuration commands

#### Configuration File

Copy the example configuration file to create your own:

```bash
cp config.example.yaml config.yaml
```

The configuration file is organized into sections:

##### Security Settings
```yaml
security:
  enable_encryption: false     # Enable end-to-end encryption
  auth_mode: "none"           # Authentication mode: none, tls, shared_key
  tls_cert_file: ""          # TLS certificate file path
  tls_key_file: ""           # TLS key file path
  trusted_peers: []          # List of trusted peer IDs
```

##### Network Topology
```yaml
network:
  max_peers: 50              # Maximum number of connected peers
  min_peers: 5               # Minimum number of connected peers
  peer_timeout: "5m"         # Peer connection timeout
  network_mode: "mesh"       # Network topology: mesh, star, hierarchical
  connection_strategy: "conservative"  # Connection strategy
  bandwidth_limit: 0         # Bandwidth limit (0 = unlimited)
```

##### Sync Strategies
```yaml
sync:
  sync_interval: "5m"        # Automatic sync interval
  sync_mode: "incremental"   # Sync mode: full, incremental, selective
  conflict_strategy: "manual" # Conflict handling strategy
  auto_sync_enabled: true    # Enable automatic syncing
  batch_size: 100           # Files to sync per batch
```

##### Merge Preferences
```yaml
merge:
  default_strategy: "manual" # Default merge strategy
  auto_resolve: false       # Auto-resolve non-conflicting changes
  ignore_whitespace: true   # Ignore whitespace in merges
  prefer_upstream: false    # Prefer upstream changes
```

##### Discovery Settings
```yaml
discovery:
  persistence_enabled: true  # Enable peer info persistence
  storage_dir: "./peer-cache" # Where to store peer information
  peer_cache_time: "24h"    # How long to cache peer info
  max_stored_peers: 1000    # Maximum cached peers
```

#### Managing Configuration

Use the `config` command to view and modify settings:

```bash
# View all configuration
gitsync config get

# View specific section
gitsync config get security
gitsync config get network
gitsync config get sync
gitsync config get merge
gitsync config get discovery

# Modify settings
gitsync config set security enable_encryption true
gitsync config set network max_peers 100
gitsync config set sync auto_sync_enabled true
gitsync config set merge default_strategy ours
```

Each configuration change is automatically persisted and applied to running instances where possible.

### Synchronization Workflow:

1.  **Initialization:** Initialize Go-GitSync in a directory to be synchronized.
2.  **Sharing:** Share the repository with other peers you want to collaborate with.
3.  **Local Changes:** Make changes to files in your local repository and commit them using standard Git commands (`git add`, `git commit`).
4.  **Automatic Synchronization:** Go-GitSync will automatically detect changes and synchronize them with connected peers in the background.
5.  **Conflict Resolution:** If conflicts arise (similar to Git merge conflicts), Go-GitSync will detect them and provide mechanisms for manual resolution.
6.  **Branching and Merging:** Use Git branching and merging workflows to manage features and collaborate effectively.

**Note:** This is a planned usage description. The actual commands and features will be implemented in future development.

## Contributing

Contributions are welcome! If you'd like to contribute to Go-GitSync, please:

1.  Fork the repository.
2.  Create a new branch for your feature or bug fix.
3.  Make your changes and commit them with clear and concise commit messages.
4.  Submit a pull request to the main repository.

Please follow the existing code style and conventions.

## License

Go-GitSync is released under the MIT License. See the `LICENSE` file for more details.

This project is under active development. Features and functionality are still being implemented. Stay tuned for updates!