// Package domain describes the existing Python harness JSON wire contracts.
// Decoding these records does not verify signatures or authorize any action.
package domain

import "encoding/json"

// ProjectItem is the normalized GitHub Project item returned by the broker.
// FieldValues keeps GitHub's heterogeneous field nodes intact.
type ProjectItem struct {
	ID          string      `json:"id"`
	Content     Issue       `json:"content"`
	FieldValues FieldValues `json:"fieldValues"`
}

// FieldValues holds provider-owned GraphQL field nodes without narrowing their schema.
type FieldValues struct {
	Nodes []json.RawMessage `json:"nodes"`
}

// Issue is the broker's normalized issue content, including dependency completeness.
type Issue struct {
	ID         string     `json:"id"`
	Number     int        `json:"number"`
	URL        string     `json:"url"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	State      string     `json:"state"`
	Type       string     `json:"type"`
	Repository Repository `json:"repository"`
	BlockedBy  Blockers   `json:"blockedBy"`
}

// Repository identifies a GitHub repository by its owner/name pair.
type Repository struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// Blockers distinguishes a verified empty dependency list from an incomplete read.
type Blockers struct {
	Nodes    []Dependency `json:"nodes"`
	Complete bool         `json:"complete"`
}

// Dependency is a native blocked-by issue returned by the broker.
type Dependency struct {
	Number     int         `json:"number"`
	URL        string      `json:"url"`
	Title      string      `json:"title"`
	State      string      `json:"state"`
	Repository *Repository `json:"repository,omitempty"`
}

// Snapshot is a version-one signed broker snapshot with Unix-second timestamps.
// Go JSON re-encoding is not the Python canonicalization used for HMAC signing.
type Snapshot struct {
	Version   int      `json:"version"`
	Repo      string   `json:"repo"`
	CreatedAt int64    `json:"created_at"`
	ExpiresAt int64    `json:"expires_at"`
	Nonce     string   `json:"nonce"`
	Actions   []Action `json:"actions"`
	Signature string   `json:"signature"`
}

// Action carries an opaque signed ID and the current broker lifecycle payload.
// Optional pointers preserve the distinction between missing and empty digests.
// Profile is a capability profile, not a concrete provider model identifier.
type Action struct {
	ID                   string  `json:"id"`
	Kind                 string  `json:"kind"`
	Issue                int     `json:"issue"`
	ItemID               string  `json:"item_id"`
	Provider             string  `json:"provider"`
	Profile              string  `json:"profile"`
	Fingerprint          string  `json:"fingerprint"`
	OriginURL            *string `json:"origin_url,omitempty"`
	BaseBranch           *string `json:"base_branch,omitempty"`
	BaseSHA              *string `json:"base_sha,omitempty"`
	Status               *string `json:"status,omitempty"`
	Branch               *string `json:"branch,omitempty"`
	Risk                 *string `json:"risk,omitempty"`
	CloneDigest          *string `json:"clone_digest,omitempty"`
	ResultDigest         *string `json:"result_digest,omitempty"`
	JobDigest            *string `json:"job_digest,omitempty"`
	ImplementationDigest *string `json:"implementation_digest,omitempty"`
	IssueFingerprint     *string `json:"issue_fingerprint,omitempty"`
	PriorResultDigest    *string `json:"prior_result_digest,omitempty"`
	PR                   *int    `json:"pr,omitempty"`
	HeadSHA              *string `json:"head_sha,omitempty"`
	ReviewDigest         *string `json:"review_digest,omitempty"`
	SecurityDigest       *string `json:"security_digest,omitempty"`
	ReviewMetaDigest     *string `json:"review_meta_digest,omitempty"`
	SecurityMetaDigest   *string `json:"security_meta_digest,omitempty"`
}

// BootstrapEnvelope records the signed plan and its durable issue bindings.
// Number is a string in the current bootstrap Project contract.
type BootstrapEnvelope struct {
	Owner       string             `json:"owner"`
	Number      string             `json:"number"`
	Plan        Plan               `json:"plan"`
	Publisher   string             `json:"publisher"`
	Nodes       map[string]Binding `json:"nodes"`
	Complete    bool               `json:"complete"`
	BootstrapID string             `json:"bootstrap_id"`
	Repo        string             `json:"repo"`
	Signature   string             `json:"signature"`
}

// Plan is the validated bootstrap planner output.
type Plan struct {
	Title   string     `json:"title"`
	Summary string     `json:"summary"`
	Nodes   []PlanNode `json:"nodes"`
}

// PlanNode describes a container or executable task, before issue publication.
type PlanNode struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Parent      *string  `json:"parent"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Acceptance  []string `json:"acceptance"`
	Validation  []string `json:"validation"`
	DependsOn   []string `json:"depends_on"`
	Risk        string   `json:"risk"`
	Priority    string   `json:"priority"`
	WorkType    string   `json:"work_type"`
}

// Binding pins a plan node to the issue identity and content digest.
type Binding struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
}

// RoutingResult is decision_engine output for task, retry, or action routing.
type RoutingResult struct {
	Engine         string              `json:"engine"`
	Model          *string             `json:"model,omitempty"`
	Kind           string              `json:"kind"`
	Decisions      map[string]Decision `json:"decisions"`
	FallbackReason *string             `json:"fallback_reason,omitempty"`
}

// Decision preserves nullable values and optional judgment probabilities.
// Raw confidence/threshold numbers distinguish absent fields from explicit nulls.
type Decision struct {
	Value         *string             `json:"value"`
	Source        string              `json:"source"`
	Confidence    json.RawMessage     `json:"confidence,omitempty"`
	Threshold     json.RawMessage     `json:"threshold,omitempty"`
	Probabilities *map[string]float64 `json:"probabilities,omitempty"`
}
