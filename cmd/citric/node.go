package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/node"
	"github.com/blaxstar/citric/internal/store"
)

// nodeFile is what 'citric node join' saves and 'citric node run' reads.
type nodeFile struct {
	Server        string   `toml:"server"`
	Name          string   `toml:"name"`
	Credential    string   `toml:"credential"`
	Labels        []string `toml:"labels,omitempty"`
	MaxConcurrent int      `toml:"max_concurrent,omitempty"`
	DataDir       string   `toml:"data_dir,omitempty"`
	CacheDir      string   `toml:"cache_dir,omitempty"`
	BuildPath     string   `toml:"build_path,omitempty"`
}

var nodeConfigPath string

func defaultNodeConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, brand.Name, "node.toml")
}

func defaultNodeDataDir() string {
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			return filepath.Join(pd, brand.Name, "node")
		}
	}
	if os.Geteuid() == 0 {
		return "/var/lib/citric-node"
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, brand.Name, "node")
	}
	return filepath.Join(os.TempDir(), "citric-node")
}

func loadNodeFile(path string) (*nodeFile, error) {
	var nf nodeFile
	if _, err := toml.DecodeFile(path, &nf); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return &nf, nil
}

func saveNodeFile(path string, nf *nodeFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(nf)
}

func defaultNodeName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "node"
	}
	h, _, _ = strings.Cut(h, ".")
	return strings.ToLower(h)
}

var cmdNode = &cobra.Command{
	Use:   "node",
	Short: "Build nodes: extra machines that run builds for a citric server",
	Long: `Build nodes run build tools for a citric server, so builds can scale past
one machine and run on other platforms (e.g. Windows). A node only makes
outbound requests to the server, so it works from behind NAT.

  on your machine, as an admin:   citric node token create
  on the node:                    citric node join --server <url> --token <token>
                                  citric node run        (or: citric node install-service)

Tools pick executors with runs_on = ["windows"] (or any label) in .citric.toml.`,
}

var cmdNodeToken = &cobra.Command{
	Use:   "token",
	Short: "Manage node join tokens (admin)",
}

var cmdNodeTokenCreate = &cobra.Command{
	Use:   "create",
	Short: "Create a one-time join token for a new node (valid 24h)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCritClient()
		if err != nil {
			return err
		}
		resp, err := c.post("/admin/nodes/join-tokens", nil)
		if err != nil {
			return err
		}
		var out struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if err := decodeOK(resp, &out); err != nil {
			return err
		}
		fmt.Printf("join token (one use, expires %s):\n%s\n\non the node:\n  %s node join --server %s --token %s\n  %s node run\n\nor with Docker:\n  docker run -d --name citric-node -e CITRIC_NODE_SERVER=%s -e CITRIC_NODE_JOIN_TOKEN=%s \\\n    -v citric-node:/var/lib/citric-node ghcr.io/blaxstar/citric:latest-full node run\n",
			out.ExpiresAt.Local().Format(time.RFC1123), out.Token,
			brand.Name, c.cfg.Server, out.Token, brand.Name, c.cfg.Server, out.Token)
		return nil
	},
}

var cmdNodeJoin = &cobra.Command{
	Use:   "join",
	Short: "Register this machine as a node using a join token",
	RunE: func(cmd *cobra.Command, args []string) error {
		server, _ := cmd.Flags().GetString("server")
		token, _ := cmd.Flags().GetString("token")
		name, _ := cmd.Flags().GetString("name")
		if server == "" || token == "" {
			return errors.New("--server and --token are required")
		}
		if name == "" {
			name = defaultNodeName()
		}
		cred, err := node.Join(server, token, name)
		if err != nil {
			return err
		}
		nf, err := loadNodeFile(nodeConfigPath)
		if err != nil {
			return err
		}
		nf.Server, nf.Name, nf.Credential = strings.TrimRight(server, "/"), name, cred
		if err := saveNodeFile(nodeConfigPath, nf); err != nil {
			return err
		}
		fmt.Printf("✓ joined %s as node %q (credential saved to %s)\nnext: %s node run   (or %s node install-service)\n",
			nf.Server, name, nodeConfigPath, brand.Name, brand.Name)
		return nil
	},
}

// resolveNodeConfig merges the node file, environment and flags (flags win),
// joining first when only a join token is available (the container path).
func resolveNodeConfig(cmd *cobra.Command) (node.Config, error) {
	nf, err := loadNodeFile(nodeConfigPath)
	if err != nil {
		return node.Config{}, err
	}
	envStr := func(key string, dst *string) {
		if v := brand.Getenv(key); v != "" {
			*dst = v
		}
	}
	envStr("NODE_SERVER", &nf.Server)
	envStr("NODE_NAME", &nf.Name)
	envStr("NODE_CREDENTIAL", &nf.Credential)
	envStr("NODE_DATA_DIR", &nf.DataDir)
	envStr("NODE_CACHE_DIR", &nf.CacheDir)
	envStr("NODE_BUILD_PATH", &nf.BuildPath)
	if v := brand.Getenv("NODE_LABELS"); v != "" {
		nf.Labels = config.SplitList(v)
	}
	if v := brand.Getenv("NODE_MAX_CONCURRENT"); v != "" {
		fmt.Sscanf(v, "%d", &nf.MaxConcurrent)
	}
	if cmd.Flags().Changed("labels") {
		v, _ := cmd.Flags().GetString("labels")
		nf.Labels = config.SplitList(v)
	}
	if cmd.Flags().Changed("max-concurrent") {
		nf.MaxConcurrent, _ = cmd.Flags().GetInt("max-concurrent")
	}
	if cmd.Flags().Changed("data-dir") {
		nf.DataDir, _ = cmd.Flags().GetString("data-dir")
	}
	if cmd.Flags().Changed("build-path") {
		nf.BuildPath, _ = cmd.Flags().GetString("build-path")
	}
	if nf.DataDir == "" {
		nf.DataDir = defaultNodeDataDir()
	}
	if nf.CacheDir == "" {
		nf.CacheDir = filepath.Join(nf.DataDir, "cache")
	}
	if nf.BuildPath == "" {
		if runtime.GOOS == "windows" {
			nf.BuildPath = os.Getenv("PATH")
		} else {
			nf.BuildPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		}
	}
	if nf.MaxConcurrent <= 0 {
		nf.MaxConcurrent = max(1, runtime.NumCPU()/2)
	}
	if nf.Server == "" {
		return node.Config{}, errors.New("no server configured: run 'citric node join' or set CITRIC_NODE_SERVER")
	}

	// Container path: a join token in the environment and no credential yet.
	// The credential is kept in the data dir (a volume) so restarts reuse it.
	if nf.Credential == "" {
		credFile := filepath.Join(nf.DataDir, "node_credential")
		if b, err := os.ReadFile(credFile); err == nil {
			parts := strings.SplitN(strings.TrimSpace(string(b)), " ", 2)
			if len(parts) == 2 {
				nf.Name, nf.Credential = parts[0], parts[1]
			}
		}
		if nf.Credential == "" {
			token := brand.Getenv("NODE_JOIN_TOKEN")
			if token == "" {
				return node.Config{}, errors.New("this node hasn't joined: run 'citric node join' or set CITRIC_NODE_JOIN_TOKEN")
			}
			if nf.Name == "" {
				nf.Name = defaultNodeName()
			}
			cred, err := node.Join(nf.Server, token, nf.Name)
			if err != nil {
				return node.Config{}, err
			}
			nf.Credential = cred
			if err := os.MkdirAll(nf.DataDir, 0o750); err != nil {
				return node.Config{}, err
			}
			if err := os.WriteFile(credFile, []byte(nf.Name+" "+cred+"\n"), 0o600); err != nil {
				return node.Config{}, err
			}
		}
	}
	return node.Config{
		Server: nf.Server, Name: nf.Name, Credential: nf.Credential, Labels: nf.Labels,
		MaxConcurrent: nf.MaxConcurrent, DataDir: nf.DataDir, CacheDir: nf.CacheDir,
		BuildPath: nf.BuildPath, Version: version,
	}, nil
}

var cmdNodeRun = &cobra.Command{
	Use:   "run",
	Short: "Run as a build node until stopped",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := resolveNodeConfig(cmd)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return node.Run(ctx, cfg)
	},
}

var cmdNodeInstallService = &cobra.Command{
	Use:   "install-service",
	Short: "Install 'citric node run' as a system service (systemd, launchd, Windows)",
	RunE: func(cmd *cobra.Command, args []string) error {
		abs, err := filepath.Abs(nodeConfigPath)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("%s not found: run 'citric node join' first", abs)
		}
		user, _ := cmd.Flags().GetString("user")
		return installService(serviceSpec{
			Name:        brand.Name + "-node",
			DisplayName: "citric build node",
			Description: "citric build node",
			Args:        []string{"node", "run", "--node-config", abs},
			User:        user,
		})
	},
}

var cmdNodeUninstallService = &cobra.Command{
	Use:   "uninstall-service",
	Short: "Stop and remove the node system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return uninstallService(brand.Name + "-node")
	},
}

var cmdNodeList = &cobra.Command{
	Use:   "list",
	Short: "List build nodes (admin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCritClient()
		if err != nil {
			return err
		}
		resp, err := c.get("/admin/nodes")
		if err != nil {
			return err
		}
		var nodes []store.Node
		if err := decodeOK(resp, &nodes); err != nil {
			return err
		}
		if len(nodes) == 0 {
			fmt.Println("no nodes — add one with 'citric node token create'")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tSTATUS\tPLATFORM\tLABELS\tJOBS\tLAST SEEN")
		for _, n := range nodes {
			seen := "never"
			state := n.Status
			if n.LastSeen != nil {
				seen = humanAgo(*n.LastSeen)
				if time.Since(*n.LastSeen) > 90*time.Second {
					state += " (offline)"
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s/%s\t%s\t%d/%d\t%s\n", n.Name, state, n.OS, n.Arch,
				strings.Join(n.Labels, ","), n.InFlight, n.MaxConcurrent, seen)
		}
		return w.Flush()
	},
}

func humanAgo(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d < time.Minute {
		return d.String() + " ago"
	}
	return t.Local().Format("Jan 2 15:04")
}

func nodeAdminAction(use, short, method, suffix string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newCritClient()
			if err != nil {
				return err
			}
			resp, err := c.request(method, "/admin/nodes/"+args[0]+suffix, nil)
			if err != nil {
				return err
			}
			if err := decodeOK(resp, nil); err != nil {
				return err
			}
			fmt.Printf("✓ %s %s\n", use, args[0])
			return nil
		},
	}
}

func init() {
	cmdNode.PersistentFlags().StringVar(&nodeConfigPath, "node-config", defaultNodeConfigPath(), "node config file (written by 'node join')")
	cmdNodeJoin.Flags().String("server", "", "citric server URL")
	cmdNodeJoin.Flags().String("token", "", "join token from 'citric node token create'")
	cmdNodeJoin.Flags().String("name", "", "node name (default: this machine's hostname)")
	cmdNodeRun.Flags().String("labels", "", "extra runs_on labels, comma-separated (os and arch are always included)")
	cmdNodeRun.Flags().Int("max-concurrent", 0, "jobs to run at once (default: half the CPUs)")
	cmdNodeRun.Flags().String("data-dir", "", "mirrors and build worktrees")
	cmdNodeRun.Flags().String("build-path", "", "PATH for build commands")
	cmdNodeInstallService.Flags().String("user", "root", "account the service runs as (systemd only)")
	cmdNodeToken.AddCommand(cmdNodeTokenCreate)
	cmdNode.AddCommand(cmdNodeToken, cmdNodeJoin, cmdNodeRun, cmdNodeList,
		nodeAdminAction("drain", "Stop sending a node new work (admin)", "POST", "/drain"),
		nodeAdminAction("resume", "Send a drained node work again (admin)", "POST", "/resume"),
		nodeAdminAction("remove", "Remove a node; its credential stops working (admin)", "DELETE", ""),
		cmdNodeInstallService, cmdNodeUninstallService)
}
