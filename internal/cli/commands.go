package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/jmendick/gitsync/internal/auth"
	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gitsync",
	Short: "A decentralized file synchronization tool using Git semantics",
	Long: `Go-GitSync is a decentralized file synchronization tool that leverages Git's 
version control semantics to provide collaborative file synchronization.`,
}

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init [owner/repo]",
	Short: "Initialize a new Go-GitSync repository",
	Long:  `Initialize a new Go-GitSync repository from GitHub. Example: gitsync init octocat/Hello-World`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid repository format. Use owner/repo format")
		}
		owner, repoName := parts[0], parts[1]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Get authenticated user
		userStore, err := auth.NewFileUserStore(filepath.Join(cfg.GetConfigDir(), "users.json"))
		if err != nil {
			return fmt.Errorf("failed to initialize user store: %w", err)
		}

		user, err := getCurrentUser(userStore)
		if err != nil {
			return fmt.Errorf("authentication required: %w", err)
		}

		gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), userStore)

		// Clone and set up permissions
		if err := gitMgr.Clone(cmd.Context(), user, owner, repoName); err != nil {
			return fmt.Errorf("failed to initialize repository: %w", err)
		}

		fmt.Printf("Successfully initialized repository: %s/%s\n", owner, repoName)
		return nil
	},
}

// shareCmd represents the share command
var shareCmd = &cobra.Command{
	Use:   "share [owner/repo]",
	Short: "Share a repository with other peers",
	Long:  `Share a repository with other peers. Example: gitsync share octocat/Hello-World`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid repository format. Use owner/repo format")
		}
		owner, repoName := parts[0], parts[1]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		userStore, err := auth.NewFileUserStore(filepath.Join(cfg.GetConfigDir(), "users.json"))
		if err != nil {
			return fmt.Errorf("failed to initialize user store: %w", err)
		}

		user, err := getCurrentUser(userStore)
		if err != nil {
			return fmt.Errorf("authentication required: %w", err)
		}

		gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), userStore)

		hasAccess, err := gitMgr.CheckAccess(user, owner, repoName)
		if err != nil {
			return fmt.Errorf("failed to check repository access: %w", err)
		}
		if !hasAccess {
			return fmt.Errorf("no access to repository %s/%s", owner, repoName)
		}

		fmt.Printf("Repository %s/%s is now available for synchronization\n", owner, repoName)
		return nil
	},
}

// syncCmd represents the sync command
var syncCmd = &cobra.Command{
	Use:   "sync [owner/repo]",
	Short: "Manually trigger synchronization for a repository",
	Long:  `Manually trigger synchronization for a repository. Example: gitsync sync octocat/Hello-World`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid repository format. Use owner/repo format")
		}
		owner, repoName := parts[0], parts[1]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		userStore, err := auth.NewFileUserStore(filepath.Join(cfg.GetConfigDir(), "users.json"))
		if err != nil {
			return fmt.Errorf("failed to initialize user store: %w", err)
		}

		user, err := getCurrentUser(userStore)
		if err != nil {
			return fmt.Errorf("authentication required: %w", err)
		}

		gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), userStore)

		hasAccess, err := gitMgr.CheckAccess(user, owner, repoName)
		if err != nil {
			return fmt.Errorf("failed to check repository access: %w", err)
		}
		if !hasAccess {
			return fmt.Errorf("no access to repository %s/%s", owner, repoName)
		}

		repo, err := gitMgr.OpenRepository(fmt.Sprintf("%s/%s", owner, repoName))
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}

		if err := gitMgr.FetchRepository(cmd.Context(), repo); err != nil {
			return fmt.Errorf("failed to sync repository: %w", err)
		}

		fmt.Printf("Successfully synchronized repository: %s/%s\n", owner, repoName)
		return nil
	},
}

// peersCmd represents the peers command
var peersCmd = &cobra.Command{
	Use:   "peers",
	Short: "List connected and known peers",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Create a temporary P2P node to discover peers
		protocolHandler := protocol.NewProtocolHandler(nil) // Temporary nil since we don't need git operations
		node, err := p2p.NewNode(cfg, protocolHandler)
		if err != nil {
			return fmt.Errorf("failed to initialize P2P node: %w", err)
		}
		defer node.Close()

		// Use the discovery service to find peers
		discovery := node.GetPeerDiscovery()
		peers := discovery.DiscoverPeers()

		if len(peers) == 0 {
			fmt.Println("No peers currently connected")
			return nil
		}

		fmt.Println("Connected peers:")
		for _, peer := range peers {
			fmt.Printf("- ID: %s\n  Addresses: %v\n", peer.ID, peer.Addresses)
		}

		return nil
	},
}

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status [owner/repo]",
	Short: "Show synchronization status of a repository",
	Long:  `Show synchronization status of a repository. Example: gitsync status octocat/Hello-World`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		parts := strings.Split(args[0], "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid repository format. Use owner/repo format")
		}
		owner, repoName := parts[0], parts[1]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		userStore, err := auth.NewFileUserStore(filepath.Join(cfg.GetConfigDir(), "users.json"))
		if err != nil {
			return fmt.Errorf("failed to initialize user store: %w", err)
		}

		user, err := getCurrentUser(userStore)
		if err != nil {
			return fmt.Errorf("authentication required: %w", err)
		}

		gitMgr := git.NewGitRepositoryManager(cfg.GetRepositoryDir(), userStore)

		hasAccess, err := gitMgr.CheckAccess(user, owner, repoName)
		if err != nil {
			return fmt.Errorf("failed to check repository access: %w", err)
		}
		if !hasAccess {
			return fmt.Errorf("no access to repository %s/%s", owner, repoName)
		}

		repo, err := gitMgr.OpenRepository(fmt.Sprintf("%s/%s", owner, repoName))
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}

		// Get current status
		headRef, err := gitMgr.GetHeadReference(repo)
		if err != nil {
			return fmt.Errorf("failed to get repository status: %w", err)
		}

		fmt.Printf("Repository: %s/%s\n", owner, repoName)
		fmt.Printf("Current commit: %s\n", headRef.Hash())
		return nil
	},
}

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display or modify configuration",
	Long: `Manage Go-GitSync configuration settings including security, network,
synchronization, and merge preferences.`,
}

// configGetCmd represents the config get command
var configGetCmd = &cobra.Command{
	Use:   "get [section]",
	Short: "Display current configuration",
	Long: `Display current configuration settings. Optionally specify a section:
- security: Security-related settings
- network: Network topology preferences
- sync: Synchronization strategies
- merge: Merge preferences
- discovery: Peer discovery settings`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// If no section specified, show all
		if len(args) == 0 {
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			fmt.Println(string(data))
			return nil
		}

		// Show specific section
		switch args[0] {
		case "security":
			printJSON(cfg.GetSecurity())
		case "network":
			printJSON(cfg.GetNetwork())
		case "sync":
			printJSON(cfg.GetSync())
		case "merge":
			printJSON(cfg.GetMerge())
		case "discovery":
			printJSON(cfg.GetDiscovery())
		default:
			return fmt.Errorf("unknown configuration section: %s", args[0])
		}

		return nil
	},
}

// configSetCmd represents the config set command
var configSetCmd = &cobra.Command{
	Use:   "set [section] [key] [value]",
	Short: "Modify configuration settings",
	Long: `Modify specific configuration settings. Usage:
gitsync config set [section] [key] [value]

Examples:
  gitsync config set security enable_encryption true
  gitsync config set network max_peers 100
  gitsync config set sync auto_sync_enabled true
  gitsync config set merge default_strategy ours`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		section, key, value := args[0], args[1], args[2]

		switch section {
		case "security":
			return setSecurityConfig(cfg, key, value)
		case "network":
			return setNetworkConfig(cfg, key, value)
		case "sync":
			return setSyncConfig(cfg, key, value)
		case "merge":
			return setMergeConfig(cfg, key, value)
		case "discovery":
			return setDiscoveryConfig(cfg, key, value)
		default:
			return fmt.Errorf("unknown configuration section: %s", section)
		}
	},
}

// Helper functions for the config commands
func printJSON(v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling data: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

func setSecurityConfig(cfg *config.Config, key, value string) error {
	return cfg.SetSecurityConfig(key, value)
}

func setNetworkConfig(cfg *config.Config, key, value string) error {
	return cfg.SetNetworkConfig(key, value)
}

func setSyncConfig(cfg *config.Config, key, value string) error {
	return cfg.SetSyncConfig(key, value)
}

func setMergeConfig(cfg *config.Config, key, value string) error {
	return cfg.SetMergeConfig(key, value)
}

func setDiscoveryConfig(cfg *config.Config, key, value string) error {
	return cfg.SetDiscoveryConfig(key, value)
}

func AddUserCommand(store auth.UserStore) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
	}

	createCmd := &cobra.Command{
		Use:   "create [username] [role]",
		Short: "Create a new user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			role := auth.Role(args[1])

			fmt.Print("Enter password: ")
			password, err := term.ReadPassword(int(syscall.Stdin))
			if err != nil {
				return err
			}
			fmt.Println()

			if err := store.CreateUser(username, string(password), role); err != nil {
				return err
			}
			fmt.Printf("User %s created successfully\n", username)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, args []string) error {
			users, err := store.ListUsers()
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tROLE\tCREATED AT\tLAST LOGIN")
			for _, user := range users {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					user.Username,
					user.Role,
					user.CreatedAt.Format(time.RFC3339),
					user.LastLoginAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete [username]",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := store.DeleteUser(args[0]); err != nil {
				return err
			}
			fmt.Printf("User %s deleted successfully\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(createCmd, listCmd, deleteCmd)
	return cmd
}

// Helper function to get the current authenticated user
func getCurrentUser(store auth.UserStore) (*auth.User, error) {
	// Try to get user from environment variable
	username := os.Getenv("GITSYNC_USER")
	if username == "" {
		return nil, fmt.Errorf("GITSYNC_USER environment variable not set. Please authenticate first")
	}

	user, err := store.GetUser(username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	if !user.IsGitHubUser() {
		return nil, fmt.Errorf("GitHub authentication required")
	}

	return user, nil
}

// loginCmd represents the login command
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login with GitHub",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if !cfg.Auth.GitHubOAuth.Enabled {
			return fmt.Errorf("GitHub OAuth is not configured. Please set up GitHub OAuth in config.yaml")
		}

		fmt.Println("Opening browser for GitHub authentication...")
		fmt.Printf("Visit: %s\n", cfg.Auth.GitHubOAuth.GetAuthURL())
		fmt.Print("Enter the callback code: ")

		var code string
		fmt.Scanln(&code)

		userStore, err := auth.NewFileUserStore(filepath.Join(cfg.GetConfigDir(), "users.json"))
		if err != nil {
			return fmt.Errorf("failed to initialize user store: %w", err)
		}

		tokenMgr := auth.NewTokenManager(cfg.Auth.TokenSecret, userStore)
		githubAuth := auth.NewGitHubOAuth(
			cfg.Auth.GitHubOAuth.ClientID,
			cfg.Auth.GitHubOAuth.ClientSecret,
			cfg.Auth.GitHubOAuth.CallbackURL,
			userStore,
			tokenMgr,
		)

		user, err := githubAuth.HandleCode(code)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Set environment variable for future commands
		os.Setenv("GITSYNC_USER", user.Username)

		fmt.Printf("Successfully logged in as %s\n", user.Username)
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	// Add config subcommands
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)

	// Add all commands to root
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(shareCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(peersCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	return rootCmd.Execute()
}

func init() {
	// Initialize user store using the new GetConfigDir function
	configDir, err := config.GetConfigDir()
	if err != nil {
		log.Printf("Warning: Failed to get config directory: %v\n", err)
		configDir = filepath.Join(".", ".gitsync") // Fallback to local directory
	}

	userStorePath := filepath.Join(configDir, "users.json")
	userStore, err := auth.NewFileUserStore(userStorePath)
	if err != nil {
		log.Printf("Warning: Failed to initialize user store: %v\n", err)
	}

	// Add user management commands if user store was initialized successfully
	if userStore != nil {
		rootCmd.AddCommand(AddUserCommand(userStore))
	}

	// Add login command
	rootCmd.AddCommand(loginCmd)

	// Add all commands to root
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(shareCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(peersCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
}
