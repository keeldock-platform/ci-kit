// Package catalog validates private feature descriptors and expands exact execution units.
package catalog

// Command is an argument-vector invocation; no shell interpolation is performed.
type Command struct {
	Cwd            string   `json:"cwd"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// Resources describes bounded local scheduling requirements.
type Resources struct {
	Weight    int      `json:"weight"`
	Exclusive []string `json:"exclusive,omitempty"`
}

// Cache permits only reviewed public dependency downloads.
type Cache struct {
	Policy string   `json:"policy"`
	Locks  []string `json:"locks,omitempty"`
}

// Outputs forbids arbitrary publication destinations.
type Outputs struct {
	Visibility string `json:"visibility"`
}

// Variant names one applicable combination rather than an implicit Cartesian product.
type Variant struct {
	ID   string            `json:"id"`
	Axes map[string]string `json:"axes"`
	Args []string          `json:"args"`
}

// ShardPolicy partitions whole tests and binds the census to measured evidence.
type ShardPolicy struct {
	Count           int        `json:"count"`
	Census          []string   `json:"census"`
	Assignments     [][]string `json:"assignments"`
	Measurement     string     `json:"measurement"`
	DurationSeconds []float64  `json:"duration_seconds"`
	Runner          string     `json:"runner"`
}

// RelatedPolicy maps inputs to whole test identities for diagnostic feedback only.
type RelatedPolicy struct {
	Runner string              `json:"runner"`
	Tests  map[string][]string `json:"tests"`
	Census []string            `json:"census"`
}

// Check owns a single check family, its inputs and execution matrix.
type Check struct {
	RequiresNetwork bool                 `json:"requires_network,omitempty"`
	Census          []string             `json:"census"`
	Paths           []string             `json:"paths"`
	TestPaths       []string             `json:"test_paths"`
	Inputs          []string             `json:"inputs"`
	Platforms       []string             `json:"platforms"`
	PlatformInputs  map[string][]string  `json:"platform_inputs,omitempty"`
	Commands        map[string][]Command `json:"commands"`
	Needs           []string             `json:"needs"`
	DependsOn       []string             `json:"depends_on"`
	Variants        []Variant            `json:"variants"`
	Shards          *ShardPolicy         `json:"shards,omitempty"`
	Related         *RelatedPolicy       `json:"related,omitempty"`
	Assertions      []string             `json:"assertions"`
	Reports         []string             `json:"reports"`
}

// Unit is the single private feature or crate descriptor.
type Unit struct {
	Schema            int                  `json:"schema"`
	ID                string               `json:"id"`
	Owner             string               `json:"owner"`
	Paths             []string             `json:"paths"`
	TestPaths         []string             `json:"test_paths"`
	Inputs            []string             `json:"inputs"`
	Platforms         []string             `json:"platforms"`
	RequiredIn        []string             `json:"required_in"`
	Commands          map[string][]Command `json:"commands,omitempty"`
	TimeoutSeconds    int                  `json:"timeout_seconds"`
	Resources         Resources            `json:"resources"`
	Needs             []string             `json:"needs"`
	DependsOn         []string             `json:"depends_on"`
	OptionalContracts []string             `json:"optional_contracts"`
	Cache             Cache                `json:"cache"`
	Assertions        []string             `json:"assertions"`
	Outputs           Outputs              `json:"outputs"`
	Pack              string               `json:"pack"`
	CostClass         string               `json:"cost_class"`
	Checks            map[string]Check     `json:"checks"`
	// Root and Digest are computed locally and never accepted from descriptor JSON.
	Root   string `json:"-"`
	Digest string `json:"-"`
}

// Entry is one exact check/platform/variant/shard execution in the bound catalogue.
type Entry struct {
	ID, Unit, Check, Platform, Variant, Shard string
	Commands                                  []Command
	Digest                                    string
}

// CheckTypes is the closed execution vocabulary.
var CheckTypes = []string{"format", "lint", "types", "unit", "integration", "e2e", "visual"}

// Platforms is the closed supported platform vocabulary.
var Platforms = []string{"linux-debian", "linux-arch", "macos-arm64", "macos-x64", "windows-x64", "windows-arm64", "web-linux", "ios-simulator", "android-emulator", "server-linux-x64", "server-linux-arm64"}
