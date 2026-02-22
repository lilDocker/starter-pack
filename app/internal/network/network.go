// Package network manages virtual networks for containers using Linux bridge
// interfaces and veth pairs.
//
// Network Architecture:
//
//   Host                            Container
//   ─────────────────────────────────────────────────────
//   lil0 (bridge 172.20.0.1/16)
//     ├── veth0abc (host side) ──── eth0 (172.20.0.2/16)
//     └── veth0def (host side) ──── eth0 (172.20.0.3/16)
//
// Each container gets:
//   - A veth pair: one end on the bridge (host), one end in the container netns
//   - An IP from the network's subnet
//   - A default gateway pointing at the bridge IP
//   - NAT via iptables MASQUERADE for outbound traffic

package network

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const networksDir = "/var/lib/lildocker/networks"

// Network represents a virtual container network
type Network struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subnet  string `json:"subnet"`  // CIDR e.g. "172.20.0.0/16"
	Gateway string `json:"gateway"` // e.g. "172.20.0.1"
	Bridge  string `json:"bridge"`  // e.g. "lil0"
	Driver  string `json:"driver"`  // "bridge" (default)
}

// Manager handles network CRUD and container attachment
type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

// Create creates a new virtual network with a bridge interface
func (m *Manager) Create(name string) (*Network, error) {
	subnet := pickSubnet()
	gateway := firstIP(subnet)
	bridgeName := "lil" + randSuffix(4)

	n := &Network{
		ID:      randSuffix(12),
		Name:    name,
		Subnet:  subnet,
		Gateway: gateway,
		Bridge:  bridgeName,
		Driver:  "bridge",
	}

	// Create the bridge interface on the host
	if err := createBridge(bridgeName, gateway, subnet); err != nil {
		return nil, fmt.Errorf("create bridge %s: %w", bridgeName, err)
	}

	// Enable NAT (masquerade) for outbound traffic
	if err := enableNAT(subnet); err != nil {
		fmt.Fprintf(os.Stderr, "warning: NAT setup failed: %v\n", err)
	}

	// Save network config
	if err := os.MkdirAll(networksDir, 0755); err != nil {
		return nil, err
	}
	data, _ := json.MarshalIndent(n, "", "  ")
	path := filepath.Join(networksDir, n.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}

	fmt.Printf("🌐 Created network %s (bridge: %s, subnet: %s)\n", name, bridgeName, subnet)
	return n, nil
}

// List returns all networks
func (m *Manager) List() ([]*Network, error) {
	entries, err := os.ReadDir(networksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var networks []*Network
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(networksDir, e.Name()))
		if err != nil {
			continue
		}
		var n Network
		if err := json.Unmarshal(data, &n); err != nil {
			continue
		}
		networks = append(networks, &n)
	}
	return networks, nil
}

// Delete removes a network and its bridge interface
func (m *Manager) Delete(name string) error {
	n, err := m.FindByName(name)
	if err != nil {
		return err
	}
	run("ip", "link", "del", n.Bridge)
	run("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", n.Subnet, "-j", "MASQUERADE")
	return os.Remove(filepath.Join(networksDir, n.ID+".json"))
}

// FindByName returns a network by name
func (m *Manager) FindByName(name string) (*Network, error) {
	all, err := m.List()
	if err != nil {
		return nil, err
	}
	for _, n := range all {
		if n.Name == name || n.ID == name {
			return n, nil
		}
	}
	return nil, fmt.Errorf("network %q not found", name)
}

// AttachContainer creates a veth pair, moves one end into the container's
// network namespace, and assigns it an IP address.
//
// containerPID is the PID of the container's init process (which defines its netns).
func (m *Manager) AttachContainer(n *Network, containerPID int, ip string) error {
	hostVeth := "veth" + randSuffix(6)
	ctrVeth := "veth" + randSuffix(6)

	// Create the veth pair
	if err := run("ip", "link", "add", hostVeth, "type", "veth", "peer", "name", ctrVeth); err != nil {
		return fmt.Errorf("create veth pair: %w", err)
	}

	// Attach host side to the bridge
	if err := run("ip", "link", "set", hostVeth, "master", n.Bridge); err != nil {
		return fmt.Errorf("attach to bridge: %w", err)
	}
	run("ip", "link", "set", hostVeth, "up")

	// Move container side into the container's network namespace
	if err := run("ip", "link", "set", ctrVeth, "netns", fmt.Sprintf("%d", containerPID)); err != nil {
		return fmt.Errorf("move veth to container netns: %w", err)
	}

	// Inside the container's netns: rename to eth0, assign IP, bring up
	prefix, _ := cidrPrefix(n.Subnet)
	nsrun(containerPID, "ip", "link", "set", ctrVeth, "name", "eth0")
	nsrun(containerPID, "ip", "addr", "add", fmt.Sprintf("%s/%s", ip, prefix), "dev", "eth0")
	nsrun(containerPID, "ip", "link", "set", "eth0", "up")
	nsrun(containerPID, "ip", "link", "set", "lo", "up")
	nsrun(containerPID, "ip", "route", "add", "default", "via", n.Gateway)

	// Set up port forwarding rules
	// (caller should set these up based on container.PortMappings)

	fmt.Printf("    → Attached to network %s with IP %s\n", n.Name, ip)
	return nil
}

// SetupPortForwarding adds iptables DNAT rules to forward host ports to container
func SetupPortForwarding(hostPort, containerPort int, containerIP, proto string) error {
	return run("iptables", "-t", "nat", "-A", "PREROUTING",
		"-p", proto,
		"--dport", fmt.Sprintf("%d", hostPort),
		"-j", "DNAT",
		"--to-destination", fmt.Sprintf("%s:%d", containerIP, containerPort),
	)
}

// AllocateIP picks the next available IP in the subnet (very simple sequential allocator)
func AllocateIP(subnet string) (string, error) {
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", err
	}
	// Start from .2 (gateway is .1)
	ip := cloneIP(ipNet.IP)
	ip[len(ip)-1] += 2
	// TODO: check for collisions against existing containers
	return ip.String(), nil
}

// --- low-level helpers ---

func createBridge(name, gateway, subnet string) error {
	run("ip", "link", "del", name) // clean up if exists
	if err := run("ip", "link", "add", name, "type", "bridge"); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", gateway+"/"+mustPrefix(subnet), "dev", name); err != nil {
		return err
	}
	return run("ip", "link", "set", name, "up")
}

func enableNAT(subnet string) error {
	// Enable IP forwarding
	os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
	// MASQUERADE outbound traffic from the subnet
	return run("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", subnet, "-j", "MASQUERADE")
}

func run(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func nsrun(pid int, args ...string) error {
	nsArgs := []string{fmt.Sprintf("--target=%d", pid), "--net", "--"}
	nsArgs = append(nsArgs, args...)
	return run(append([]string{"nsenter"}, nsArgs...)...)
}

func randSuffix(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func pickSubnet() string {
	// Use 172.20.x.0/24 range, pick a random /24
	x := rand.Intn(254) + 1
	return fmt.Sprintf("172.20.%d.0/24", x)
}

func firstIP(subnet string) string {
	_, ipNet, _ := net.ParseCIDR(subnet)
	ip := cloneIP(ipNet.IP)
	ip[len(ip)-1]++
	return ip.String()
}

func cloneIP(ip net.IP) net.IP {
	clone := make(net.IP, len(ip))
	copy(clone, ip)
	return clone
}

func cidrPrefix(cidr string) (string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", err
	}
	ones, _ := ipNet.Mask.Size()
	return fmt.Sprintf("%d", ones), nil
}

func mustPrefix(cidr string) string {
	p, _ := cidrPrefix(cidr)
	return p
}
