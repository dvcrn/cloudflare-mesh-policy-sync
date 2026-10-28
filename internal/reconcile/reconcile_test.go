package reconcile

import (
	"slices"
	"testing"
	"time"

	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/cloudflare"
)

var now = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func node(name string, inactiveFor time.Duration, clientIDs ...string) cloudflare.Connector {
	n := cloudflare.Connector{Name: name}
	if inactiveFor > 0 {
		t := now.Add(-inactiveFor)
		n.InactiveAt = &t
	}
	for _, id := range clientIDs {
		n.Connections = append(n.Connections, struct {
			ClientID string `json:"client_id"`
		}{id})
	}
	return n
}

var regs = []cloudflare.Registration{
	{ID: "r-alpha", IPv4: "100.96.0.1", IPv6: "2606:4700:cf1:1000::1"},
	{ID: "r-beta", IPv4: "100.96.0.2", IPv6: "2606:4700:cf1:1000::2"},
	{ID: "r-gamma", IPv4: "100.96.0.3", IPv6: "2606:4700:cf1:1000::3"},
}

func TestDesired(t *testing.T) {
	nodes := []cloudflare.Connector{
		node("alpha.mesh", 0, "r-alpha"),
		node("Beta.mesh", 0, "unknown", "r-beta"),
		node("delta.mesh", 2*time.Hour),
		node("old.mesh", 30*24*time.Hour),
		node("orphan.mesh", 0, "unknown"),
	}
	got, keep := Desired(nodes, regs, []string{"mesh@example.com", "me@example.com"}, "mesh.d.pn", now, 7*24*time.Hour)

	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	if want := []string{"mesh.d.pn: alpha", "mesh.d.pn: beta"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	alpha := got[0]
	if !slices.Equal(alpha.RuleSettings.OverrideIPs, []string{"100.96.0.1", "2606:4700:cf1:1000::1"}) {
		t.Fatalf("alpha ips = %v", alpha.RuleSettings.OverrideIPs)
	}
	if alpha.Traffic != `any(dns.domains[*] == "alpha.mesh.d.pn")` {
		t.Fatalf("traffic = %s", alpha.Traffic)
	}
	if want := `identity.email in {"mesh@example.com" "me@example.com"}`; alpha.Identity != want {
		t.Fatalf("identity = %s, want %s", alpha.Identity, want)
	}
	if want := map[string]bool{"mesh.d.pn: delta": true, "mesh.d.pn: orphan": true}; len(keep) != len(want) || !keep["mesh.d.pn: delta"] || !keep["mesh.d.pn: orphan"] {
		t.Fatalf("keep = %v, want %v", keep, want)
	}
}

func TestLabel(t *testing.T) {
	for in, want := range map[string]string{
		"alpha":              "alpha",
		"My-Laptop.local":    "my-laptop-local",
		"build-vm-123456789": "build-vm-123456789",
		"  weird__name ":     "weird-name",
		"---":                "",
	} {
		if got := Label(in); got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlan(t *testing.T) {
	desired, keep := Desired([]cloudflare.Connector{
		node("alpha.mesh", 0, "r-alpha"),
		node("beta.mesh", 0, "r-beta"),
		node("gamma.mesh", 0, "r-gamma"),
		node("delta.mesh", time.Hour),
	}, regs, []string{"mesh@example.com"}, "mesh.d.pn", now, 7*24*time.Hour)

	byName := map[string]cloudflare.Rule{}
	for _, r := range desired {
		byName[r.Name] = r
	}
	keepAlpha := byName["mesh.d.pn: alpha"]
	keepAlpha.ID, keepAlpha.Precedence = "keep", 200
	stale := byName["mesh.d.pn: beta"]
	stale.ID, stale.Precedence = "stale", 300
	stale.RuleSettings.OverrideIPs = []string{"100.96.0.99"}
	dup := keepAlpha
	dup.ID = "dup"
	current := []cloudflare.Rule{
		keepAlpha, stale, dup,
		{ID: "down", Name: "mesh.d.pn: delta"},
		{ID: "gone", Name: "mesh.d.pn: build-vm-123456789"},
		{ID: "foreign", Name: "Block hosts"},
		{ID: "resolver", Name: "Mesh nodes: upstream via 1.1.1.1"},
	}
	create, update, del := Plan(desired, keep, current, "mesh.d.pn")

	if len(create) != 1 || create[0].Name != "mesh.d.pn: gamma" {
		t.Fatalf("create = %v, want gamma", create)
	}
	if len(update) != 1 || update[0].ID != "stale" || update[0].Precedence != 300 || update[0].RuleSettings.OverrideIPs[0] != "100.96.0.2" {
		t.Fatalf("update = %+v, want beta with its ID, precedence and new IPs", update)
	}
	var ids []string
	for _, r := range del {
		ids = append(ids, r.ID)
	}
	if want := []string{"dup", "gone"}; !slices.Equal(ids, want) {
		t.Fatalf("delete = %v, want %v", ids, want)
	}
}
