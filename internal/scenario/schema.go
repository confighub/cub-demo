// Package scenario defines the scenario file that drives cub-demo, loads it,
// and expands it into the concrete set of clusters, spaces and units to create.
// Expansion is pure and deterministic: the same scenario always yields the same
// model, which is what lets "plan" describe exactly what "up" will do without
// contacting a server.
package scenario

// Scenario is the top-level document. The YAML field names are the schema
// users write; see scenarios/meridian.yaml for the worked example.
type Scenario struct {
	// Name is the DemoName label stamped on every entity and the prefix of the
	// home space. It must be a valid slug.
	Name string `yaml:"name"`
	// Context optionally pins the scenario to one cub context name. Mutating
	// commands refuse to run when the active context differs — the guard that
	// keeps "cub demo down --demo workflows" from firing into the meridian org.
	Context string `yaml:"context"`
	// Company, Domain and Cloud are the fiction: they feed manifest templates
	// and target facts. Cloud is the provider name recorded as Cloud.Provider.
	Company string `yaml:"company"`
	Domain  string `yaml:"domain"`
	Cloud   string `yaml:"cloud"`

	Regions     []Region `yaml:"regions"`
	Classes     []Class  `yaml:"classes"`
	Departments []string `yaml:"departments"`

	Clusters  ClusterSpec `yaml:"clusters"`
	Catalog   []Component `yaml:"catalog"`
	Workloads []Component `yaml:"workloads"`

	Story     Story     `yaml:"story"`
	Workflows Workflows `yaml:"workflows"`

	// Plays are the scenario's named moves, run with "cub demo play <name>".
	// A step is either a shell line (run:) or a call to one of the tool's
	// generic primitives (call: + with:). Strings are Go templates over
	// present.PlayContext plus the variables earlier steps export (bump-image
	// exports .Version), and everything runs with CUB_CONTEXT pinned to
	// Context, so a move cannot hit the wrong org.
	//
	// This is the tool/scenario boundary: the tool ships capabilities
	// (observe, invoke, bump-image, changeorder); the scenario names the
	// actors and beats — its "ci" and "argobot" are plays here, not verbs of
	// the tool.
	Plays map[string][]PlayStep `yaml:"plays"`
}

// PlayStep is one step of a play: exactly one of Run (a shell line) or Call
// (a primitive name, parameterized by With).
type PlayStep struct {
	Run  string         `yaml:"run,omitempty"`
	Call string         `yaml:"call,omitempty"`
	With map[string]any `yaml:"with,omitempty"`
}

// Region is a location clusters live in.
type Region struct {
	Name string `yaml:"name"`
	// ProviderRegion is the cloud's own name for it (us-east-1), recorded as
	// the Cloud.Region fact and used for cloud-resource placement.
	ProviderRegion string `yaml:"providerRegion"`
	DNSZone        string `yaml:"dnsZone"`
	Locality       string `yaml:"locality"`
}

// Class is an environment class (dev, test, uat, prod). Classes are listed in
// promotion order. The policy knobs are what a class base changes relative to
// the root base; component.yaml function lists refer to them by name.
type Class struct {
	Name      string `yaml:"name"`
	Replicas  int    `yaml:"replicas"`
	LogLevel  string `yaml:"logLevel"`
	Size      string `yaml:"size"`
	DBClass   string `yaml:"dbClass"`
	MultiAZ   bool   `yaml:"multiAZ"`
	NodeCount int    `yaml:"nodeCount"`
	// Values are free-form extra knobs available to templates and function
	// arguments as .Class.Values.<key>.
	Values map[string]string `yaml:"values"`
}

// ClusterSpec is either generated from rules or listed explicitly. Rules and
// List can be combined; explicit entries are added after the generated ones.
type ClusterSpec struct {
	// KubernetesVersions are assigned to clusters deterministically, so that
	// version is a fact worth filtering on.
	KubernetesVersions []string      `yaml:"kubernetesVersions"`
	Rules              []ClusterRule `yaml:"rules"`
	List               []ClusterDef  `yaml:"list"`
}

// ClusterRule generates Count[region] clusters of one class for one department
// (the empty department or "shared" means shared clusters). The "default" key
// applies to every region not named explicitly.
type ClusterRule struct {
	Class      string         `yaml:"class"`
	Department string         `yaml:"department"`
	Count      map[string]int `yaml:"count"`
}

// ClusterDef is one explicitly listed cluster. Name is optional; the default
// follows the generated naming.
type ClusterDef struct {
	Name              string `yaml:"name"`
	Region            string `yaml:"region"`
	Class             string `yaml:"class"`
	Department        string `yaml:"department"`
	Index             int    `yaml:"index"`
	KubernetesVersion string `yaml:"kubernetesVersion"`
	NodeCount         int    `yaml:"nodeCount"`
}

// Component is a platform-catalog entry or a business workload. Which list it
// appears in decides its Layer label.
type Component struct {
	Name string `yaml:"name"`
	// Owner is the team; Department the business unit (workloads) or empty
	// (platform).
	Owner      string `yaml:"owner"`
	Department string `yaml:"department"`
	// Dir is the component directory (relative to the scenario's manifests
	// root) holding component.yaml and the manifest files.
	Dir string `yaml:"dir"`
	// Placement selects the clusters the component is deployed to: the union
	// of its rules. Empty means every cluster.
	Placement []PlacementRule `yaml:"placement"`
	// External are cloud resources the workload needs; each becomes a unit.
	External []External `yaml:"external"`

	// LiveStatus overrides how the livestatus phase paints this component's
	// deployments. "none" paints nothing, so a healthy gate is only ever
	// satisfied after an observe play reports; empty means the fleet default.
	LiveStatus string `yaml:"liveStatus"`
}

// PlacementRule selects clusters by intersection of its non-empty fields;
// Exclude removes matches.
type PlacementRule struct {
	Classes     []string  `yaml:"classes"`
	Regions     []string  `yaml:"regions"`
	Departments []string  `yaml:"departments"`
	MaxIndex    int       `yaml:"maxIndex"`
	Exclude     *Selector `yaml:"exclude"`
}

// Selector is a plain set of dimension values.
type Selector struct {
	Classes     []string `yaml:"classes"`
	Regions     []string `yaml:"regions"`
	Departments []string `yaml:"departments"`
}

// External is a cloud resource modelled as a Crossplane managed resource.
// Kind selects the manifest template (rds, bucket, queue, cache).
type External struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// Story is the deliberate imperfection that makes the fleet look alive. All
// selections are deterministic from Seed.
type Story struct {
	Seed       int64      `yaml:"seed"`
	LiveStatus LiveStatus `yaml:"liveStatus"`
	Unreleased Unreleased `yaml:"unreleased"`
	Skews      []Skew     `yaml:"skews"`
}

// LiveStatus sets the share of deployments reported in each unhealthy state.
type LiveStatus struct {
	DegradedPercent    int `yaml:"degradedPercent"`
	OutOfSyncPercent   int `yaml:"outOfSyncPercent"`
	ProgressingPercent int `yaml:"progressingPercent"`
}

// Unreleased is the share of deployments left with an unreleased head revision.
type Unreleased struct {
	Percent int `yaml:"percent"`
}

// Skew is a function applied to one unit of the deployments a selector picks,
// after the releases, so those deployments show unreleased changes that differ
// from their siblings.
type Skew struct {
	Component string   `yaml:"component"`
	Unit      string   `yaml:"unit"`
	Function  string   `yaml:"function"`
	Args      []string `yaml:"args"`
	Where     Selector `yaml:"where"`
}

// Workflows is the change-workflow part of a scenario. The tool generates one
// ChangeWorkflow definition per component in the bases-first shape — a single
// "bases" stage carrying every class base, then one stage per class's
// deployments, gated released+healthy on the class before it. The product
// intends a stage to span hierarchy levels eventually; until that is
// specified, bases-first is the shape that works and reads honestly
// (docs/DESIGN.md).
type Workflows struct {
	// Disabled skips workflow and change-order seeding entirely.
	Disabled bool `yaml:"disabled"`
	// Prerequisites overrides the gates per deployment stage (keyed by class
	// name, plus "final"), e.g. {test: [released], prod: [released, healthy]}.
	// Absent keys default to [released, healthy] for every deployment stage
	// after the first and for final. The first class must stay gate-free:
	// its previous stage holds only bases, which release nothing and report
	// no health.
	Prerequisites map[string][]string `yaml:"prerequisites"`
	ChangeOrders  []ChangeOrder       `yaml:"changeOrders"`
}

// ChangeOrder is an in-flight change to seed: a function applied to one root
// base unit, then promoted through the component's workflow up to (and
// including) LandedThrough, releasing and reporting healthy at each
// deployment stage so the next stage's gates hold.
type ChangeOrder struct {
	Component   string   `yaml:"component"`
	Slug        string   `yaml:"slug"`
	Description string   `yaml:"description"`
	Unit        string   `yaml:"unit"`
	Function    string   `yaml:"function"`
	Args        []string `yaml:"args"`
	// LandedThrough is a stage name: "bases" or a class name.
	LandedThrough string `yaml:"landedThrough"`
	// Degrade paints this many of the LandedThrough stage's deployments
	// Degraded after their release, so the next stage is visibly blocked on
	// the healthy gate.
	Degrade int `yaml:"degrade"`
}

// ComponentSpec is the component.yaml inside a component directory: the units
// the component consists of and the variation applied per class, region and
// cluster.
type ComponentSpec struct {
	// Units are created in the root base in this order, one per manifest file.
	Units []UnitSpec `yaml:"units"`
	// Values are template inputs for the manifests, available as .Values.
	Values map[string]string `yaml:"values"`
	// PerClass, PerRegion and PerCluster are functions applied to the class
	// bases, the deployments of a region, and each deployment respectively.
	// Function arguments are templates over .Class, .Region and .Cluster.
	PerClass   map[string][]FunctionCall `yaml:"perClass"`
	PerRegion  []FunctionCall            `yaml:"perRegion"`
	PerCluster []FunctionCall            `yaml:"perCluster"`
	// PerVariant is applied to one named deployment (the cluster's variant of
	// this component): the local override a story needs on one cluster only.
	PerVariant map[string][]FunctionCall `yaml:"perVariant"`

	// Protect marks paths as local overrides a merge must not overwrite, per
	// class: on the class base and on every deployment of that class, because
	// protection lives on a unit's MutationSources and does not survive cloning.
	Protect map[string][]Protection `yaml:"protect"`

	// Release names what the bump-image primitive bumps: the unit and the
	// container that carry the component's own image. A component without one
	// cannot be shipped by a play.
	Release *ReleaseSpec `yaml:"release"`
}

// UnitSpec is one unit of a component.
type UnitSpec struct {
	Slug string `yaml:"slug"`
	File string `yaml:"file"`
}

// FunctionCall is a ConfigHub function invocation. Where narrows the units it
// applies to within the selected spaces (a unit where-clause); empty means all.
type FunctionCall struct {
	Function string   `yaml:"function"`
	Args     []string `yaml:"args"`
	Where    string   `yaml:"where"`
}

// Protection is one unit's protected paths. Each path is
// RESOURCE_TYPE:RESOURCE_NAME:PATH, the form "cub unit set-protection" takes,
// e.g. "apps/v1/Deployment:catalog-api/catalog-api-api:spec.template.spec.containers.0.resources.limits.memory".
type Protection struct {
	Unit  string   `yaml:"unit"`
	Paths []string `yaml:"paths"`
	// Variants narrows the protection to these deployments (cluster names) and
	// leaves the class base alone; empty means the class base and every
	// deployment of the class.
	Variants []string `yaml:"variants"`
}

// ReleaseSpec is the image a version bump targets.
type ReleaseSpec struct {
	Unit      string `yaml:"unit"`
	Container string `yaml:"container"`
}
