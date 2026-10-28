package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/cloudflare"
)

const description = "Managed by cloudflare-mesh-dns-sync"

// Desired returns one override rule per connected Mesh node, answering <label>.<domain> with the
// node's Mesh IPv4 and IPv6 for the allowed identities. The label is the node name without ".mesh".
// A disconnected node can't be matched to its registration, so its rule name goes into keep
// instead, until it has been disconnected for longer than staleAfter.
func Desired(nodes []cloudflare.Connector, regs []cloudflare.Registration, allowed []string, domain string, now time.Time, staleAfter time.Duration) (rules []cloudflare.Rule, keep map[string]bool) {
	byID := map[string]cloudflare.Registration{}
	for _, r := range regs {
		byID[r.ID] = r
	}
	identity := identityExpr(allowed)
	keep = map[string]bool{}
	seen := map[string]bool{}
	for _, n := range nodes {
		label := Label(strings.TrimSuffix(strings.ToLower(n.Name), ".mesh"))
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		name := RuleName(domain, label)

		var ips []string
		for _, c := range n.Connections {
			if r, ok := byID[c.ClientID]; ok {
				for _, ip := range []string{r.IPv4, r.IPv6} {
					if ip != "" {
						ips = append(ips, ip)
					}
				}
				break
			}
		}
		if len(ips) == 0 {
			if n.InactiveAt == nil || now.Sub(*n.InactiveAt) < staleAfter {
				keep[name] = true
			}
			continue
		}
		fqdn := label + "." + strings.ToLower(domain)
		rules = append(rules, cloudflare.Rule{
			Name:         name,
			Description:  description,
			Enabled:      true,
			Action:       "override",
			Filters:      []string{"dns"},
			Traffic:      `any(dns.domains[*] == ` + strconv.Quote(fqdn) + `)`,
			Identity:     identity,
			RuleSettings: cloudflare.RuleSettings{OverrideIPs: ips},
		})
	}
	slices.SortFunc(rules, func(a, b cloudflare.Rule) int { return strings.Compare(a.Name, b.Name) })
	return rules, keep
}

// Label turns a node name into a DNS label: lowercase, with runs of
// characters outside [a-z0-9-] collapsed to a single hyphen.
func Label(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// RuleName is the Gateway rule name for a label. The "<domain>: " prefix marks rules the sync owns.
func RuleName(domain, label string) string {
	return strings.ToLower(domain) + ": " + label
}

func identityExpr(allowed []string) string {
	quoted := make([]string, len(allowed))
	for i, e := range allowed {
		quoted[i] = strconv.Quote(e)
	}
	return "identity.email in {" + strings.Join(quoted, " ") + "}"
}

// Plan diffs desired against current. Every rule named "<domain>: ..." is owned by the sync,
// so owned rules that are neither desired nor kept get deleted; other rules are never touched.
func Plan(desired []cloudflare.Rule, keep map[string]bool, current []cloudflare.Rule, domain string) (create, update, del []cloudflare.Rule) {
	prefix := strings.ToLower(domain) + ": "
	have := map[string]cloudflare.Rule{}
	for _, r := range current {
		if !strings.HasPrefix(strings.ToLower(r.Name), prefix) {
			continue
		}
		if _, dup := have[r.Name]; dup || (!keep[r.Name] && !slices.ContainsFunc(desired, func(d cloudflare.Rule) bool { return d.Name == r.Name })) {
			del = append(del, r)
			continue
		}
		have[r.Name] = r
	}
	for _, d := range desired {
		cur, ok := have[d.Name]
		switch {
		case !ok:
			create = append(create, d)
		case !same(cur, d):
			d.ID, d.Precedence = cur.ID, cur.Precedence
			update = append(update, d)
		}
	}
	return create, update, del
}

func same(a, b cloudflare.Rule) bool {
	return a.Enabled == b.Enabled && a.Action == b.Action && a.Traffic == b.Traffic &&
		a.Identity == b.Identity && a.Description == b.Description &&
		slices.Equal(a.Filters, b.Filters) && slices.Equal(a.RuleSettings.OverrideIPs, b.RuleSettings.OverrideIPs)
}

type Syncer struct {
	Cloudflare *cloudflare.Client
	Domain     string
	// Allowed are the identities whose DNS queries get the private answers.
	Allowed []string
	// StaleAfter deletes the rule of a node disconnected for this long. @default 168h
	StaleAfter time.Duration
	// DryRun logs planned changes without applying them. @default false
	DryRun bool
}

func (s *Syncer) Run(ctx context.Context) error {
	nodes, err := s.Cloudflare.Connectors(ctx)
	if err != nil {
		return fmt.Errorf("list mesh nodes: %w", err)
	}
	// A token without Cloudflare One Connectors Read gets an empty list instead of a 403,
	// which would otherwise delete every managed rule.
	if len(nodes) == 0 {
		return errors.New("no mesh nodes listed; check the token has Cloudflare One Connectors Read")
	}
	regs, err := s.Cloudflare.Registrations(ctx)
	if err != nil {
		return fmt.Errorf("list registrations: %w", err)
	}
	current, err := s.Cloudflare.Rules(ctx)
	if err != nil {
		return fmt.Errorf("list gateway rules: %w", err)
	}
	desired, keep := Desired(nodes, regs, s.Allowed, s.Domain, time.Now(), s.StaleAfter)
	create, update, del := Plan(desired, keep, current, s.Domain)
	log.Printf("nodes=%d registrations=%d rules=%d desired=%d kept=%d create=%d update=%d delete=%d dry_run=%t",
		len(nodes), len(regs), len(current), len(desired), len(keep), len(create), len(update), len(del), s.DryRun)

	if s.DryRun {
		changed := map[string]bool{}
		for _, r := range append(create, update...) {
			changed[r.Name] = true
		}
		for _, r := range desired {
			if !changed[r.Name] {
				log.Printf("ok     %s -> %v", r.Name, r.RuleSettings.OverrideIPs)
			}
		}
		for _, r := range current {
			if keep[r.Name] {
				log.Printf("keep   %s (node disconnected)", r.Name)
			}
		}
	}

	// Creating before deleting means a renamed node briefly resolves under both names instead of neither.
	var errs []error
	for _, r := range create {
		log.Printf("create %s -> %v", r.Name, r.RuleSettings.OverrideIPs)
		if !s.DryRun {
			if err := s.Cloudflare.CreateRule(ctx, r); err != nil {
				errs = append(errs, fmt.Errorf("create %s: %w", r.Name, err))
			}
		}
	}
	for _, r := range update {
		log.Printf("update %s -> %v", r.Name, r.RuleSettings.OverrideIPs)
		if !s.DryRun {
			if err := s.Cloudflare.UpdateRule(ctx, r); err != nil {
				errs = append(errs, fmt.Errorf("update %s: %w", r.Name, err))
			}
		}
	}
	for _, r := range del {
		log.Printf("delete %s -> %v", r.Name, r.RuleSettings.OverrideIPs)
		if !s.DryRun {
			if err := s.Cloudflare.DeleteRule(ctx, r.ID); err != nil {
				errs = append(errs, fmt.Errorf("delete %s: %w", r.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}
