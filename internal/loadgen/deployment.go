package loadgen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Deployment is what a host-side observation of the running containers established.
//
// The decision this implements, with the alternatives it rejects, is ADR-0003
// (docs/decisions/0003-deployed-artifact-identity.md). The summary below is why *this file*
// looks as it does; the ADR is why the approach was chosen at all.
//
// **Why this is a file and not a field of `/meta`.** The measurement-contract §11 image
// identity is a fact about the deployed *artifact*, and a process cannot observe which image
// wraps it. Asking
// the service would only have it repeat an environment variable back, which is asserted
// provenance standing next to a compiler-observed commit SHA under names that do not say
// which is which — the shape of the PR1 defect where `commit_sha` named the generator rather
// than the service under test.
//
// So the observation is taken where it can actually be made, by inspecting the live
// containers from the host (`test/scripts/record-deployment.sh`), and travels to the run as
// a file. That also keeps ag-sept-plan §6.3 intact: `alloca-load` holds no credentials and
// speaks only HTTP, and a Docker socket is root on the host — the last thing a generator that
// must later move to separate compute should hold. The file crosses that boundary; the socket
// does not.
//
// What it cannot do is prove itself. An operator who hand-writes this file gets whatever
// they wrote, exactly as `generator_location` is a declaration the manifest takes at face
// value. The claim is narrower and worth stating plainly: on the documented path the value
// is read from the running deployment rather than asserted about it.
type Deployment struct {
	// ImageID is the immutable content identity every unit must share. For a locally built
	// image this is Docker's own image ID; a registry digest exists only after a push, which
	// is why PR3b can record identity at all without a registry.
	ImageID string `json:"image_id"`
	// ImageTag is the human-readable alias, kept for operators and never relied on: a tag is
	// mutable, and two different builds can wear the same one.
	ImageTag string `json:"image_tag,omitempty"`
	// Units maps each inspected container to what was observed about it, so a disagreement
	// can name the container to go and look at rather than only the fact of it.
	Units map[string]DeploymentUnit `json:"units"`
}

// DeploymentUnit is one container's observation: the artifact it is running, and the address
// it serves on.
//
// The target is what binds this record to the run. Without it the record says only "some two
// containers shared an image", which is a claim about the host rather than about the
// deployment the run actually addressed — an observation of a topology raised earlier, or of
// units nothing routed to, would satisfy it just as well.
type DeploymentUnit struct {
	// ImageID is the image the container was created from, read from the container rather
	// than from the tag it was launched by: a tag can be repointed after the start, the
	// created-from image cannot.
	ImageID string `json:"image_id"`
	// Target is the base URL this container publishes, observed from its port binding. It is
	// what `Deployment.BindTo` matches against the run's routed targets.
	Target string `json:"target"`
}

// LoadDeployment reads a deployment record and refuses one that cannot support a claim.
//
// The disagreement check is the point of recording per-unit IDs at all. Units running
// different images is precisely the failure the commit SHA cannot see: same code, different
// artifact — a stale `:dev` tag left pointing at an older build, or a rebuild on a newer base
// layer. Nothing in the request totals would reveal it, and the run would report one image
// identity for a deployment that had two.
func LoadDeployment(path string) (Deployment, error) {
	var d Deployment

	raw, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("reading deployment record: %w", err)
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("decoding deployment record %s: %w", path, err)
	}

	if len(d.Units) == 0 {
		return d, fmt.Errorf("deployment record %s names no units: it cannot say what the run "+
			"was served by", path)
	}

	names := make([]string, 0, len(d.Units))
	for name := range d.Units {
		names = append(names, name)
	}
	sort.Strings(names)

	// Two containers cannot serve one address. If the record says they do, one of the two
	// observations is stale — a container recreated on a new port while the record still
	// carries the old one — and the run cannot tell which unit it actually reached.
	claimedBy := make(map[string]string, len(names))
	for _, name := range names {
		unit := d.Units[name]
		if unit.ImageID == "" {
			return d, fmt.Errorf("deployment record %s gives no image id for %q", path, name)
		}
		if unit.Target == "" {
			return d, fmt.Errorf("deployment record %s gives no target for %q: without one the "+
				"record cannot be matched to the units the run addressed", path, name)
		}
		key, err := normalizeTarget(unit.Target)
		if err != nil {
			return d, fmt.Errorf("deployment record %s gives an unusable target for %q: %w",
				path, name, err)
		}
		if other, taken := claimedBy[key]; taken {
			return d, fmt.Errorf("deployment record %s has %s and %s both serving %s: one of the "+
				"two observations is stale, and the run cannot tell which unit it reached",
				path, other, name, unit.Target)
		}
		claimedBy[key] = name
	}

	first := names[0]
	for _, name := range names[1:] {
		if d.Units[name].ImageID != d.Units[first].ImageID {
			return d, fmt.Errorf("the units are not running one image: %s is on %s and %s is on "+
				"%s. The commit SHA cannot see this — the same code served from a stale tag or "+
				"rebuilt on a different base layer carries the same revision — so a run across "+
				"them would record one image identity for a deployment that had two",
				first, short(d.Units[first].ImageID), name, short(d.Units[name].ImageID))
		}
	}

	if d.ImageID == "" {
		d.ImageID = d.Units[first].ImageID
	}
	if d.ImageID != d.Units[first].ImageID {
		return d, fmt.Errorf("deployment record %s claims image %s but its units are running %s",
			path, short(d.ImageID), short(d.Units[first].ImageID))
	}
	return d, nil
}

// BindTo requires the observation to describe exactly the units this run will address.
//
// This is what turns "some containers on the host share an image" into "the units this run
// routed to share an image". Without it the record is unbound evidence: a topology raised
// yesterday, a second stack on other ports, or a unit nothing routes to would all satisfy the
// image check while saying nothing about the deployment under measurement.
//
// The correspondence must be exact in both directions, and each direction fails for its own
// reason. An **unobserved** target is a unit the run drove whose artifact is unknown, which is
// the whole gap §11 exists to close. An **unrouted** observation means the record describes a
// topology other than the one measured — most often a stale file — and a record that is wrong
// about the unit set is not evidence that its image identity is right either.
func (d Deployment) BindTo(targets []string) error {
	if len(targets) == 0 {
		return fmt.Errorf("no routed targets to bind the deployment record to")
	}

	observed := make(map[string]string, len(d.Units))
	for name, unit := range d.Units {
		key, err := normalizeTarget(unit.Target)
		if err != nil {
			return fmt.Errorf("deployment record gives an unusable target for %q: %w", name, err)
		}
		observed[key] = name
	}

	var unobserved []string
	routed := make(map[string]bool, len(targets))
	for _, target := range targets {
		key, err := normalizeTarget(target)
		if err != nil {
			return fmt.Errorf("the run routes to an unusable target %q: %w", target, err)
		}
		if routed[key] {
			continue
		}
		routed[key] = true
		if _, seen := observed[key]; !seen {
			unobserved = append(unobserved, target)
		}
	}

	var unrouted []string
	for key, name := range observed {
		if !routed[key] {
			unrouted = append(unrouted, fmt.Sprintf("%s (%s)", name, d.Units[name].Target))
		}
	}
	sort.Strings(unobserved)
	sort.Strings(unrouted)

	switch {
	case len(unobserved) > 0 && len(unrouted) > 0:
		return fmt.Errorf("the deployment record describes a different topology from the one "+
			"this run addresses: nothing was observed for %s, and %s was observed but is not "+
			"routed to. Re-record against the running topology before measuring",
			strings.Join(unobserved, ", "), strings.Join(unrouted, ", "))
	case len(unobserved) > 0:
		return fmt.Errorf("the deployment record does not cover every unit this run addresses: "+
			"nothing was observed for %s, so the run would drive an artifact it cannot name — "+
			"which is exactly what service_commit_sha does not cover",
			strings.Join(unobserved, ", "))
	case len(unrouted) > 0:
		return fmt.Errorf("the deployment record observed %s, which this run does not route to: "+
			"the record describes a topology other than the one being measured, most often "+
			"because it was recorded before the topology was raised again",
			strings.Join(unrouted, ", "))
	}
	return nil
}

// normalizeTarget reduces a base URL to the scheme, host and port a comparison should turn
// on, so that the loopback spellings an operator and a container runtime each prefer do not
// read as different units.
//
// `localhost`, `127.0.0.1` and `::1` are the same interface, and Docker reports a published
// binding on `0.0.0.0`, which from the generator's side is reached as loopback. Path, query
// and credentials are dropped: they do not identify a unit, and leaving them in would make an
// otherwise-matching record fail for a trailing slash.
func normalizeTarget(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%q needs a scheme and host, as in http://localhost:8081", raw)
	}

	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "127.0.0.1", "::1", "0.0.0.0", "":
		host = "localhost"
	}

	port := parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return fmt.Sprintf("%s://%s:%s", strings.ToLower(parsed.Scheme), host, port), nil
}

// short renders an image id for a message: enough to compare by eye, not the whole digest.
func short(id string) string {
	trimmed := strings.TrimPrefix(id, "sha256:")
	if len(trimmed) > 12 {
		return trimmed[:12]
	}
	return trimmed
}
