// Package cmd implements the CLI commands for lildocker.
package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/you/lildocker/internal/builder"
	"github.com/you/lildocker/internal/container"
	"github.com/you/lildocker/internal/image"
	"github.com/you/lildocker/internal/network"
	"github.com/you/lildocker/internal/parser"
)

var (
	store   = image.NewStore()
	manager = container.NewManager(store)
	netmgr  = network.NewManager()
)

func init() {
	if err := store.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "storage init failed: %v\n", err)
		os.Exit(1)
	}
}

// Build handles: lildocker build -t name[:tag] [context]
func Build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	nameTag := fs.String("t", "", "Image name and optional tag (name:tag)")
	verbose := fs.Bool("v", false, "Verbose output")
	fs.Parse(args)

	if *nameTag == "" {
		fmt.Fprintln(os.Stderr, "build: -t <name[:tag]> is required")
		os.Exit(1)
	}

	contextPath := "."
	if fs.NArg() > 0 {
		contextPath = fs.Arg(0)
	}

	// Find the lilDockfile
	dfPath := filepath.Join(contextPath, "lilDockfile")
	if _, err := os.Stat(dfPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "build: no lilDockfile found in %s\n", contextPath)
		os.Exit(1)
	}

	// Parse
	df, err := parser.Parse(dfPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		os.Exit(1)
	}

	if *verbose {
		df.Print()
	}

	// Build
	name, tag := splitNameTag(*nameTag)
	b := builder.New(store, contextPath, name, tag, *verbose)
	_, err = b.Build(df)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
		os.Exit(1)
	}
}

// Run handles: lildocker run [options] <image> [cmd...]
func Run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	detach := fs.Bool("d", false, "Run container in background")
	name := fs.String("name", "", "Container name")
	hostname := fs.String("h", "", "Container hostname")
	memLimit := fs.String("m", "", "Memory limit e.g. 256m, 1g")
	cpuShares := fs.Int64("cpu-shares", 0, "CPU shares (relative weight)")
	netName := fs.String("net", "", "Connect to network by name")
	envFlags := newStringSlice()
	fs.Var(envFlags, "e", "Environment variable KEY=VALUE (repeatable)")
	portFlags := newStringSlice()
	fs.Var(portFlags, "p", "Port mapping hostPort:containerPort (repeatable)")
	volFlags := newStringSlice()
	fs.Var(volFlags, "v", "Volume mount host:container[:ro] (repeatable)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "run: image name required")
		os.Exit(1)
	}

	imageNameTag := fs.Arg(0)
	cmd := fs.Args()[1:]

	// Resolve image
	manifest, err := store.FindByName(imageNameTag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}

	// Parse memory limit
	var memBytes int64
	if *memLimit != "" {
		memBytes = parseMemory(*memLimit)
	}

	// Parse port mappings  e.g. "8080:80"
	var ports []container.PortMapping
	for _, p := range *portFlags {
		pm, err := parsePortMapping(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run: bad port mapping %q: %v\n", p, err)
			os.Exit(1)
		}
		ports = append(ports, pm)
	}

	// Parse volume mounts  e.g. "/host/path:/container/path:ro"
	var volumes []container.VolumeMount
	for _, v := range *volFlags {
		vm, err := parseVolumeMount(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run: bad volume %q: %v\n", v, err)
			os.Exit(1)
		}
		volumes = append(volumes, vm)
	}

	// Resolve network
	var netID string
	if *netName != "" {
		n, err := netmgr.FindByName(*netName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "run: network %q not found\n", *netName)
			os.Exit(1)
		}
		netID = n.ID
	}

	opts := container.RunOptions{
		Name:      *name,
		Detach:    *detach,
		Ports:     ports,
		Volumes:   volumes,
		Env:       *envFlags,
		Cmd:       cmd,
		MemLimit:  memBytes,
		CPUShares: *cpuShares,
		NetworkID: netID,
		Hostname:  *hostname,
	}

	cfg, err := manager.Create(manifest, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: create container: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🚀 Starting container %s\n", cfg.ID[:12])

	if err := manager.Start(cfg, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "run: start container: %v\n", err)
		os.Exit(1)
	}
}

// Images handles: lildocker images
func Images() {
	images, err := store.ListImages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "images: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "IMAGE ID\tNAME\tTAG\tSIZE\tCREATED")
	for _, m := range images {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.ID[:12],
			m.Name,
			m.Tag,
			humanSize(m.Size),
			humanTime(m.CreatedAt),
		)
	}
	w.Flush()
}

// Ps handles: lildocker ps
func Ps() {
	containers, err := manager.ListContainers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ps: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tCOMMAND\tSTATE\tCREATED\tNAME")
	for _, c := range containers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.ID[:12],
			c.ImageName,
			strings.Join(c.Cmd, " "),
			c.State,
			humanTime(c.CreatedAt),
			c.Name,
		)
	}
	w.Flush()
}

// Stop handles: lildocker stop <id>
func Stop(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "stop: container id required")
		os.Exit(1)
	}
	if err := manager.Stop(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "stop: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Stopped %s\n", args[0])
}

// Rm handles: lildocker rm <id>
func Rm(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rm: container id required")
		os.Exit(1)
	}
	if err := manager.Remove(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "rm: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed %s\n", args[0])
}

// Rmi handles: lildocker rmi <image>
func Rmi(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "rmi: image name required")
		os.Exit(1)
	}
	if err := store.DeleteImage(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "rmi: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed image %s\n", args[0])
}

// Exec handles: lildocker exec <id> <cmd...>
func Exec(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "exec: container id and command required")
		os.Exit(1)
	}
	if err := manager.Exec(args[0], args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "exec: %v\n", err)
		os.Exit(1)
	}
}

// Logs handles: lildocker logs <id>
func Logs(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "logs: container id required")
		os.Exit(1)
	}
	if err := manager.Logs(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "logs: %v\n", err)
		os.Exit(1)
	}
}

// Network handles: lildocker network <subcommand>
func Network(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "network: subcommand required (ls, create, rm)")
		os.Exit(1)
	}
	switch args[0] {
	case "ls":
		networks, _ := netmgr.List()
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		fmt.Fprintln(w, "NETWORK ID\tNAME\tSUBNET\tGATEWAY\tBRIDGE")
		for _, n := range networks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n.ID[:8], n.Name, n.Subnet, n.Gateway, n.Bridge)
		}
		w.Flush()

	case "create":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "network create: name required")
			os.Exit(1)
		}
		n, err := netmgr.Create(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "network create: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(n.ID)

	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "network rm: name required")
			os.Exit(1)
		}
		if err := netmgr.Delete(args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "network rm: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "network: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// Child is the re-exec entry point that runs inside the container namespaces
func Child(args []string) {
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "_child: internal error: insufficient args")
		os.Exit(1)
	}
	cfgJSON := args[0]
	rootDir := args[1]
	logFile := args[2]
	cmd := args[3:]

	if err := container.Child(cfgJSON, rootDir, logFile, cmd); err != nil {
		fmt.Fprintf(os.Stderr, "container: %v\n", err)
		os.Exit(1)
	}
}

// --- parsing helpers ---

func parsePortMapping(s string) (container.PortMapping, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return container.PortMapping{}, fmt.Errorf("expected hostPort:containerPort")
	}
	host, err := strconv.Atoi(parts[0])
	if err != nil {
		return container.PortMapping{}, err
	}
	ctr, err := strconv.Atoi(parts[1])
	if err != nil {
		return container.PortMapping{}, err
	}
	return container.PortMapping{HostPort: host, ContainerPort: ctr, Protocol: "tcp"}, nil
}

func parseVolumeMount(s string) (container.VolumeMount, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return container.VolumeMount{}, fmt.Errorf("expected host:container[:ro]")
	}
	vm := container.VolumeMount{
		HostPath:      parts[0],
		ContainerPath: parts[1],
	}
	if len(parts) == 3 && parts[2] == "ro" {
		vm.ReadOnly = true
	}
	return vm, nil
}

func parseMemory(s string) int64 {
	s = strings.ToLower(strings.TrimSpace(s))
	multiplier := int64(1)
	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	}
	n, _ := strconv.ParseInt(s, 10, 64)
	return n * multiplier
}

func splitNameTag(s string) (string, string) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s, "latest"
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	} else if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("2006-01-02")
}

// stringSlice is a flag.Value for repeated -e, -p, -v flags
type stringSlice []string

func newStringSlice() *stringSlice { s := stringSlice{}; return &s }
func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
