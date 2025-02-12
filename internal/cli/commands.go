package cli

import (
	"fmt"

	"github.com/jmendick/gitsync/internal/config"
	"github.com/jmendick/gitsync/internal/git"
	"github.com/jmendick/gitsync/internal/p2p"
	"github.com/jmendick/gitsync/internal/p2p/protocol"
	"github.com/spf13/cobra"
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
	Use:   "init [repository-name]",
	Short: "Initialize a new Go-GitSync repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoName := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		gitMgr, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
		if err != nil {
			return fmt.Errorf("failed to create git manager: %w", err)
		}

		_, err = gitMgr.OpenRepository(repoName)
		if err != nil {
			return fmt.Errorf("failed to initialize repository: %w", err)
		}

		fmt.Printf("Successfully initialized repository: %s\n", repoName)
		return nil
	},
}

// shareCmd represents the share command
var shareCmd = &cobra.Command{
	Use:   "share [repository-name]",
	Short: "Share a repository with other peers",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoName := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Verify repository exists by trying to open it
		gitMgr, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
		if err != nil {
			return fmt.Errorf("failed to create git manager: %w", err)
		}

		repo, err := gitMgr.OpenRepository(repoName)
		if err != nil {
			return fmt.Errorf("repository not found: %w", err)
		}

		// Get repository status to ensure it's valid
		_, err = gitMgr.GetHeadReference(repo)
		if err != nil {
			return fmt.Errorf("invalid repository state: %w", err)
		}

		fmt.Printf("Repository %s is now available for synchronization\n", repoName)
		return nil
	},
}

// syncCmd represents the sync command
var syncCmd = &cobra.Command{
	Use:   "sync [repository-name]",
	Short: "Manually trigger synchronization for a repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoName := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		gitMgr, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
		if err != nil {
			return fmt.Errorf("failed to create git manager: %w", err)
		}

		repo, err := gitMgr.OpenRepository(repoName)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}

		err = gitMgr.FetchRepository(cmd.Context(), repo)
		if err != nil {
			return fmt.Errorf("failed to sync repository: %w", err)
		}

		fmt.Printf("Successfully synchronized repository: %s\n", repoName)
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
	Use:   "status [repository-name]",
	Short: "Show synchronization status of a repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoName := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		gitMgr, err := git.NewGitRepositoryManager(cfg.GetRepositoryDir())
		if err != nil {
			return fmt.Errorf("failed to create git manager: %w", err)
		}

		repo, err := gitMgr.OpenRepository(repoName)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}

		// Get current status
		headRef, err := gitMgr.GetHeadReference(repo)
		if err != nil {
			return fmt.Errorf("failed to get repository status: %w", err)
		}

		fmt.Printf("Repository: %s\n", repoName)
		fmt.Printf("Current commit: %s\n", headRef.Hash())
		return nil
	},
}

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display or modify configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		fmt.Printf("Current configuration:\n")
		fmt.Printf("Listen address: %s\n", cfg.GetListenAddress())
		fmt.Printf("Repository directory: %s\n", cfg.GetRepositoryDir())
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(shareCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(peersCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(configCmd)
	return rootCmd.Execute()
}
