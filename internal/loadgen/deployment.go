package loadgen

import (
	"encoding/json"
	"fmt"
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
// **Why this is a file and not a field of `/meta`.** The plan's §6.4 image identity is a fact
// about the deployed *artifact*, and a process cannot observe which image wraps it. Asking
// the service would only have it repeat an environment variable back, which is asserted
// provenance standing next to a compiler-observed commit SHA under names that do not say
// which is which — the shape of the PR1 defect where `commit_sha` named the generator rather
// than the service under test.
//
// So the observation is taken where it can actually be made, by inspecting the live
// containers from the host (`test/scripts/record-deployment.sh`), and travels to the run as
// a file. That also keeps §6.3 intact: `alloca-load` holds no credentials and speaks only
// HTTP, and a Docker socket is root on the host — the last thing a generator that must later
// move to separate compute should hold. The file crosses that boundary; the socket does not.
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
	// Units maps each inspected container to the image ID it was running, so a disagreement
	// can name the container to go and look at rather than only the fact of it.
	Units map[string]string `json:"units"`
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

	for _, name := range names {
		if d.Units[name] == "" {
			return d, fmt.Errorf("deployment record %s gives no image id for %q", path, name)
		}
	}

	first := names[0]
	for _, name := range names[1:] {
		if d.Units[name] != d.Units[first] {
			return d, fmt.Errorf("the units are not running one image: %s is on %s and %s is on "+
				"%s. The commit SHA cannot see this — the same code served from a stale tag or "+
				"rebuilt on a different base layer carries the same revision — so a run across "+
				"them would record one image identity for a deployment that had two",
				first, short(d.Units[first]), name, short(d.Units[name]))
		}
	}

	if d.ImageID == "" {
		d.ImageID = d.Units[first]
	}
	if d.ImageID != d.Units[first] {
		return d, fmt.Errorf("deployment record %s claims image %s but its units are running %s",
			path, short(d.ImageID), short(d.Units[first]))
	}
	return d, nil
}

// short renders an image id for a message: enough to compare by eye, not the whole digest.
func short(id string) string {
	trimmed := strings.TrimPrefix(id, "sha256:")
	if len(trimmed) > 12 {
		return trimmed[:12]
	}
	return trimmed
}
