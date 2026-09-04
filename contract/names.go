package contract

// The ops-log and projection resource names, declared once for every
// service (hits-hq/02-DESIGN/hits-up.md § boundaries). They carry no
// prefix and no knob: one HITS per account or JetStream domain
// (decision 0004). A service declaring its own copy is a defect.
const (
	// OpsStream is the ops-log stream — the source of record.
	OpsStream = "hits-ops"
	// OpsSubjects is the subject space the stream captures.
	OpsSubjects = "hits.ops.>"
	// ItemOpsPrefix + item ID is the subject an item's ops append to.
	ItemOpsPrefix = "hits.ops.item."
	// ItemOpsSubjects filters the stream down to item ops.
	ItemOpsSubjects = "hits.ops.item.>"
	// ProjectOpsPrefix + slug is the subject a project's ops append to.
	ProjectOpsPrefix = "hits.ops.project."
	// StateBucket holds the state projection — item snapshots, the
	// located-in vocabulary, and operational keys, all folds of the log,
	// told apart by key prefix (decision 0012).
	StateBucket = "hits-state"
)
